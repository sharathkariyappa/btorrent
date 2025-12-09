package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// TorrentInfo represents torrent information for the frontend
type TorrentInfo struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	InfoHash      string     `json:"infoHash"`
	Size          int64      `json:"size"`
	SizeStr       string     `json:"sizeStr"`
	Progress      float64    `json:"progress"`
	Status        string     `json:"status"`
	DownloadSpeed int64      `json:"downloadSpeed"`
	UploadSpeed   int64      `json:"uploadSpeed"`
	DownloadedStr string     `json:"downloadSpeedStr"`
	UploadedStr   string     `json:"uploadSpeedStr"`
	Peers         int        `json:"peers"`
	Seeds         int        `json:"seeds"`
	ETA           string     `json:"eta"`
	Files         []FileInfo `json:"files"`
	AddedAt       time.Time  `json:"addedAt"`
}

// FileInfo represents file information within a torrent
type FileInfo struct {
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	SizeStr  string  `json:"sizeStr"`
	Progress float64 `json:"progress"`
	Path     string  `json:"path"`
}

// Stats represents global statistics
type Stats struct {
	TotalDownloadSpeed string `json:"totalDownload"`
	TotalUploadSpeed   string `json:"totalUpload"`
	ActiveTorrents     int    `json:"activeTorrents"`
	TotalPeers         int    `json:"totalPeers"`
}

// speedTracker tracks download/upload speeds
type speedTracker struct {
	lastBytes int64
	lastTime  time.Time
	speed     int64
}

// App struct
type App struct {
	ctx            context.Context
	client         *torrent.Client
	torrents       map[string]*torrent.Torrent
	torrentsMutex  sync.RWMutex
	downloadDir    string
	downloadSpeeds map[string]*speedTracker
	uploadSpeeds   map[string]*speedTracker
	speedsMutex    sync.RWMutex
	depositAddress string
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		torrents:       make(map[string]*torrent.Torrent),
		downloadSpeeds: make(map[string]*speedTracker),
		uploadSpeeds:   make(map[string]*speedTracker),
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Setup download directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("Error getting home directory: %v", err)
		homeDir = "."
	}
	a.downloadDir = filepath.Join(homeDir, "TorrentFlow", "Downloads")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(a.downloadDir, 0755); err != nil {
		log.Printf("Error creating download directory: %v", err)
		wailsruntime.LogError(ctx, fmt.Sprintf("Failed to create download directory: %v", err))
		return
	}

	// Configure torrent client
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = a.downloadDir
	cfg.Seed = true
	cfg.Debug = false
	cfg.DisableIPv6 = false
	cfg.NoDHT = false      // Enable DHT for magnet links
	cfg.ListenPort = 42069 // Set a default port

	// Use file storage
	cfg.DefaultStorage = storage.NewFile(a.downloadDir)

	// Create client
	client, err := torrent.NewClient(cfg)
	if err != nil {
		log.Printf("Error creating torrent client: %v", err)
		wailsruntime.LogError(ctx, fmt.Sprintf("Failed to create torrent client: %v", err))
		return
	}
	a.client = client

	// Start stats update loop
	go a.updateStatsLoop()

	log.Printf("✓ Torrent client initialized successfully")
	log.Printf("✓ Download folder: %s", a.downloadDir)
	wailsruntime.LogInfo(ctx, fmt.Sprintf("Torrent client ready - Downloads: %s", a.downloadDir))
}

// shutdown is called when the app stops
func (a *App) shutdown(ctx context.Context) {
	if a.client != nil {
		log.Println("Closing torrent client...")
		a.client.Close()
		log.Println("✓ Torrent client closed")
	}
}

// AddMagnet adds a torrent from a magnet link
func (a *App) AddMagnet(magnetURI string) error {
	if a.client == nil {
		return fmt.Errorf("torrent client not initialized")
	}

	t, err := a.client.AddMagnet(magnetURI)
	if err != nil {
		return fmt.Errorf("failed to add magnet: %w", err)
	}

	hash := t.InfoHash().String()

	// Initialize speed trackers immediately
	a.speedsMutex.Lock()
	a.downloadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
	a.uploadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
	a.speedsMutex.Unlock()

	// Add to torrents map immediately (even before getting info)
	a.torrentsMutex.Lock()
	a.torrents[hash] = t
	a.torrentsMutex.Unlock()

	log.Printf("Added magnet link, waiting for metadata...")

	// Wait for info with timeout in background
	go func() {
		select {
		case <-t.GotInfo():
			log.Printf("✓ Got metadata for torrent: %s", t.Name())
			t.DownloadAll()
			wailsruntime.EventsEmit(a.ctx, "torrent-added", hash)
		case <-time.After(60 * time.Second):
			log.Printf("⚠ Timeout waiting for torrent metadata")
			wailsruntime.LogWarning(a.ctx, "Could not fetch torrent metadata within 60 seconds")
		}
	}()

	return nil
}

func (a *App) AddLocalFiles(paths []string) error {
	if a.client == nil {
		return fmt.Errorf("torrent client not initialized")
	}

	if len(paths) == 0 {
		return fmt.Errorf("no files provided")
	}

	// Step 1: Build metainfo (torrent metadata)
	info := metainfo.Info{
		Name:        filepath.Base(paths[0]), // torrent name
		PieceLength: 256 * 1024,              // 256 KB pieces
	}

	// If multiple files, we need to create File slices
	var files []metainfo.FileInfo
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("failed to stat file %s: %w", p, err)
		}

		files = append(files, metainfo.FileInfo{
			Path:   []string{filepath.Base(p)},
			Length: fi.Size(),
		})
	}
	info.Files = files

	// Build MetaInfo object
	mi := &metainfo.MetaInfo{
		AnnounceList: [][]string{
			{"udp://tracker.openbittorrent.com:80/announce"},
		},
	}

	// Step 2: Add torrent to client
	t, err := a.client.AddTorrent(mi)
	if err != nil {
		return fmt.Errorf("failed to add local files torrent: %w", err)
	}

	hash := t.InfoHash().String()

	// Initialize speed trackers
	a.speedsMutex.Lock()
	a.downloadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
	a.uploadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
	a.speedsMutex.Unlock()

	// Add to torrents map
	a.torrentsMutex.Lock()
	a.torrents[hash] = t
	a.torrentsMutex.Unlock()

	// Start seeding
	t.Seeding()

	log.Printf("Started seeding local files: %v", paths)
	// runtime.EventsEmit(a.ctx, "torrent-added", hash)

	return nil
}

// AddTorrentFile adds a torrent from a file
func (a *App) AddTorrentFile(filePath string) error {
	if a.client == nil {
		return fmt.Errorf("torrent client not initialized")
	}

	mi, err := metainfo.LoadFromFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to load torrent file: %w", err)
	}

	t, err := a.client.AddTorrent(mi)
	if err != nil {
		return fmt.Errorf("failed to add torrent: %w", err)
	}

	hash := t.InfoHash().String()

	// Initialize speed trackers
	a.speedsMutex.Lock()
	a.downloadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
	a.uploadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
	a.speedsMutex.Unlock()

	t.DownloadAll()

	a.torrentsMutex.Lock()
	a.torrents[hash] = t
	a.torrentsMutex.Unlock()

	log.Printf("✓ Added torrent file: %s", t.Name())
	wailsruntime.EventsEmit(a.ctx, "torrent-added", hash)

	return nil
}

// GetTorrents returns all torrents
func (a *App) GetTorrents() []TorrentInfo {
	a.torrentsMutex.RLock()
	defer a.torrentsMutex.RUnlock()

	var torrents []TorrentInfo
	for hash, t := range a.torrents {
		info := a.getTorrentInfo(hash, t)
		torrents = append(torrents, info)
	}

	return torrents
}

// GetTorrent returns a single torrent by hash
func (a *App) GetTorrent(infoHash string) (TorrentInfo, error) {
	a.torrentsMutex.RLock()
	defer a.torrentsMutex.RUnlock()

	t, exists := a.torrents[infoHash]
	if !exists {
		return TorrentInfo{}, fmt.Errorf("torrent not found")
	}

	return a.getTorrentInfo(infoHash, t), nil
}

// PauseTorrent pauses a torrent
func (a *App) PauseTorrent(infoHash string) error {
	a.torrentsMutex.RLock()
	t, exists := a.torrents[infoHash]
	a.torrentsMutex.RUnlock()

	if !exists {
		return fmt.Errorf("torrent not found")
	}

	t.CancelPieces(0, t.NumPieces())
	log.Printf("⏸ Paused torrent: %s", t.Name())
	return nil
}

// ResumeTorrent resumes a torrent
func (a *App) ResumeTorrent(infoHash string) error {
	a.torrentsMutex.RLock()
	t, exists := a.torrents[infoHash]
	a.torrentsMutex.RUnlock()

	if !exists {
		return fmt.Errorf("torrent not found")
	}

	t.DownloadAll()
	log.Printf("▶ Resumed torrent: %s", t.Name())
	return nil
}

// RemoveTorrent removes a torrent
func (a *App) RemoveTorrent(infoHash string, deleteFiles bool) error {
	a.torrentsMutex.Lock()
	t, exists := a.torrents[infoHash]
	if exists {
		delete(a.torrents, infoHash)
	}
	a.torrentsMutex.Unlock()

	if !exists {
		return fmt.Errorf("torrent not found")
	}

	torrentName := t.Name()

	// Clean up speed trackers
	a.speedsMutex.Lock()
	delete(a.downloadSpeeds, infoHash)
	delete(a.uploadSpeeds, infoHash)
	a.speedsMutex.Unlock()

	t.Drop()

	if deleteFiles && t.Info() != nil {
		// Delete files
		for _, file := range t.Files() {
			path := filepath.Join(a.downloadDir, file.Path())
			if err := os.Remove(path); err != nil {
				log.Printf("Warning: failed to delete file %s: %v", path, err)
			}
		}
		log.Printf("🗑 Removed torrent and deleted files: %s", torrentName)
	} else {
		log.Printf("🗑 Removed torrent: %s", torrentName)
	}

	return nil
}

// GetStats returns global statistics
func (a *App) GetStats() Stats {
	a.torrentsMutex.RLock()
	defer a.torrentsMutex.RUnlock()

	var totalDown, totalUp int64
	var activeTorrents, totalPeers int

	a.speedsMutex.RLock()
	for hash := range a.torrents {
		if tracker, ok := a.downloadSpeeds[hash]; ok {
			totalDown += tracker.speed
		}
		if tracker, ok := a.uploadSpeeds[hash]; ok {
			totalUp += tracker.speed
		}
	}
	a.speedsMutex.RUnlock()

	for _, t := range a.torrents {
		stats := t.Stats()

		if t.BytesCompleted() < t.Length() {
			activeTorrents++
		}

		totalPeers += stats.ActivePeers
	}

	return Stats{
		TotalDownloadSpeed: formatSpeed(totalDown),
		TotalUploadSpeed:   formatSpeed(totalUp),
		ActiveTorrents:     activeTorrents,
		TotalPeers:         totalPeers,
	}
}

// OpenDownloadFolder opens the download folder in the system file manager
func (a *App) OpenDownloadFolder() error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "explorer"
		args = []string{a.downloadDir}
	case "darwin":
		cmd = "open"
		args = []string{a.downloadDir}
	default: // linux
		cmd = "xdg-open"
		args = []string{a.downloadDir}
	}

	if err := exec.Command(cmd, args...).Start(); err != nil {
		log.Printf("Error opening folder: %v", err)
		return fmt.Errorf("failed to open download folder: %w", err)
	}

	log.Printf("📁 Opened download folder: %s", a.downloadDir)
	return nil
}

// SelectTorrentFile opens a file picker for torrent files
func (a *App) SelectTorrentFile() (string, error) {
	file, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select Torrent File",
		Filters: []wailsruntime.FileFilter{
			{
				DisplayName: "Torrent Files (*.torrent)",
				Pattern:     "*.torrent",
			},
		},
	})

	if err != nil {
		return "", err
	}

	return file, nil
}

// Helper functions

func (a *App) getTorrentInfo(hash string, t *torrent.Torrent) TorrentInfo {
	stats := t.Stats()

	// Determine status
	status := a.getTorrentStatus(t, stats)

	// Calculate progress
	progress := 0.0
	if t.Length() > 0 {
		progress = float64(t.BytesCompleted()) / float64(t.Length()) * 100
	}

	// Get files info
	var files []FileInfo
	if t.Info() != nil {
		for _, file := range t.Files() {
			fileProgress := 0.0
			if file.Length() > 0 {
				fileProgress = float64(file.BytesCompleted()) / float64(file.Length()) * 100
			}

			files = append(files, FileInfo{
				Name:     file.DisplayPath(),
				Size:     file.Length(),
				SizeStr:  formatBytes(file.Length()),
				Progress: fileProgress,
				Path:     file.Path(),
			})
		}
	}

	// Get speed from tracker
	var downloadSpeed, uploadSpeed int64
	a.speedsMutex.RLock()
	if tracker, ok := a.downloadSpeeds[hash]; ok {
		downloadSpeed = tracker.speed
	}
	if tracker, ok := a.uploadSpeeds[hash]; ok {
		uploadSpeed = tracker.speed
	}
	a.speedsMutex.RUnlock()

	// Calculate ETA
	eta := "Unknown"
	if downloadSpeed > 0 && t.BytesCompleted() < t.Length() {
		remaining := t.Length() - t.BytesCompleted()
		seconds := remaining / downloadSpeed
		eta = formatDuration(time.Duration(seconds) * time.Second)
	}

	// Get torrent name (handle case where info isn't available yet)
	name := t.Name()
	if name == "" {
		name = "Loading metadata..."
	}

	return TorrentInfo{
		ID:            hash,
		Name:          name,
		InfoHash:      hash,
		Size:          t.Length(),
		SizeStr:       formatBytes(t.Length()),
		Progress:      progress,
		Status:        status,
		DownloadSpeed: downloadSpeed,
		UploadSpeed:   uploadSpeed,
		DownloadedStr: formatSpeed(downloadSpeed),
		UploadedStr:   formatSpeed(uploadSpeed),
		Peers:         stats.ActivePeers,
		Seeds:         stats.ConnectedSeeders,
		ETA:           eta,
		Files:         files,
		AddedAt:       time.Now(),
	}
}

// getTorrentStatus determines the current status of a torrent
func (a *App) getTorrentStatus(t *torrent.Torrent, stats torrent.TorrentStats) string {
	// Check if download is complete
	if t.BytesCompleted() >= t.Length() {
		if stats.ActivePeers > 0 {
			return "seeding"
		}
		return "completed"
	}

	// Check if downloading
	if stats.ActivePeers == 0 {
		if stats.TotalPeers > 0 {
			return "stalled" // Has peers but not connected
		}
		return "paused"
	}

	return "downloading"
}

func (a *App) updateStatsLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Update speed trackers
		a.torrentsMutex.RLock()
		for hash, t := range a.torrents {
			stats := t.Stats()
			now := time.Now()

			// Update download speed
			a.speedsMutex.Lock()
			if tracker, ok := a.downloadSpeeds[hash]; ok {
				elapsed := now.Sub(tracker.lastTime).Seconds()
				if elapsed > 0 {
					currentBytes := stats.BytesReadData.Int64()
					bytesDiff := currentBytes - tracker.lastBytes
					tracker.speed = int64(float64(bytesDiff) / elapsed)
					tracker.lastBytes = currentBytes
					tracker.lastTime = now
				}
			}

			// Update upload speed
			if tracker, ok := a.uploadSpeeds[hash]; ok {
				elapsed := now.Sub(tracker.lastTime).Seconds()
				if elapsed > 0 {
					currentBytes := stats.BytesWrittenData.Int64()
					bytesDiff := currentBytes - tracker.lastBytes
					tracker.speed = int64(float64(bytesDiff) / elapsed)
					tracker.lastBytes = currentBytes
					tracker.lastTime = now
				}
			}
			a.speedsMutex.Unlock()
		}
		a.torrentsMutex.RUnlock()

		// Emit update event
		torrents := a.GetTorrents()
		stats := a.GetStats()

		data := map[string]interface{}{
			"torrents": torrents,
			"stats":    stats,
		}

		jsonData, _ := json.Marshal(data)
		wailsruntime.EventsEmit(a.ctx, "torrents-update", string(jsonData))
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func formatSpeed(bytesPerSec int64) string {
	return formatBytes(bytesPerSec) + "/s"
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// SetDepositAddress saves the user deposit BSV address
func (a *App) SetDepositAddress(address string) error {
	a.depositAddress = address
	return nil
}

// GetDepositAddress returns the saved BSV deposit address
func (a *App) GetDepositAddress() (string, error) {
	return a.depositAddress, nil
}
func (a *App) SelectLocalFiles() ([]string, error) {
	files, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select Files",
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// CreateTorrentFromFiles creates a torrent from selected local files
func (a *App) CreateTorrentFromFiles(files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("no files provided")
	}

	// TODO: Implement actual torrent creation logic if needed
	// For now, we just log the files
	log.Printf("Creating torrent from files: %v", files)
	wailsruntime.LogInfo(a.ctx, fmt.Sprintf("Creating torrent from files: %v", files))

	return nil
}

// GetBalance returns the user balance (mocked for now)
func (a *App) GetBalance() (float64, error) {
	// TODO: Replace with actual balance fetching logic if needed
	balance := 0.0
	log.Printf("Fetching balance: %f", balance)
	return balance, nil
}

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "Btorrent - Modern Torrent Client",
		Width:  1400,
		Height: 900,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 8, G: 27, B: 42, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
