package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
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
	IsPaused      bool       `json:"isPaused"`
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

// TorrentState represents saved torrent state for persistence
type TorrentState struct {
	InfoHash  string    `json:"infoHash"`
	MagnetURI string    `json:"magnetUri,omitempty"`
	IsPaused  bool      `json:"isPaused"`
	AddedAt   time.Time `json:"addedAt"`
}

// App struct
type App struct {
	ctx            context.Context
	client         *torrent.Client
	torrents       map[string]*torrent.Torrent
	torrentsMutex  sync.RWMutex
	downloadDir    string
	stateFile      string
	downloadSpeeds map[string]*speedTracker
	uploadSpeeds   map[string]*speedTracker
	speedsMutex    sync.RWMutex
	pausedTorrents map[string]bool
	pausedMutex    sync.RWMutex
	depositAddress string
	lastUpdateHash string
	lastUpdateTime time.Time
	updateMutex    sync.Mutex
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		torrents:       make(map[string]*torrent.Torrent),
		downloadSpeeds: make(map[string]*speedTracker),
		uploadSpeeds:   make(map[string]*speedTracker),
		pausedTorrents: make(map[string]bool),
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
	a.stateFile = filepath.Join(homeDir, "TorrentFlow", "torrents.json")

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
	cfg.NoDHT = false
	cfg.ListenPort = 42069
	cfg.DefaultStorage = storage.NewFile(a.downloadDir)

	// Create client
	client, err := torrent.NewClient(cfg)
	if err != nil {
		log.Printf("Error creating torrent client: %v", err)
		wailsruntime.LogError(ctx, fmt.Sprintf("Failed to create torrent client: %v", err))
		return
	}
	a.client = client

	// Load saved torrents
	a.loadSavedTorrents()

	// Start stats update loop
	go a.updateStatsLoop()

	log.Printf("✓ Torrent client initialized successfully")
	log.Printf("✓ Download folder: %s", a.downloadDir)
	wailsruntime.LogInfo(ctx, fmt.Sprintf("Torrent client ready - Downloads: %s", a.downloadDir))
}

// shutdown is called when the app stops
func (a *App) shutdown(ctx context.Context) {
	// Save torrent states before closing
	a.saveTorrentStates()

	if a.client != nil {
		log.Println("Closing torrent client...")
		a.client.Close()
		log.Println("✓ Torrent client closed")
	}
}

// saveTorrentStates saves current torrent states to disk
func (a *App) saveTorrentStates() {
	a.torrentsMutex.RLock()
	defer a.torrentsMutex.RUnlock()

	var states []TorrentState
	for hash, t := range a.torrents {
		// Try to get magnet URI
		var magnetURI string
		if t.Info() != nil {
			mi := metainfo.MetaInfo{
				InfoBytes: bencode.MustMarshal(*t.Info()),
			}
			mag, _ := mi.MagnetV2()
			magnetURI = mag.String()
		}

		a.pausedMutex.RLock()
		isPaused := a.pausedTorrents[hash]
		a.pausedMutex.RUnlock()

		states = append(states, TorrentState{
			InfoHash:  hash,
			MagnetURI: magnetURI,
			IsPaused:  isPaused,
			AddedAt:   time.Now(),
		})
	}

	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		log.Printf("Error marshaling torrent states: %v", err)
		return
	}

	if err := os.WriteFile(a.stateFile, data, 0644); err != nil {
		log.Printf("Error saving torrent states: %v", err)
		return
	}

	log.Printf("✓ Saved %d torrent states", len(states))
}

// loadSavedTorrents loads previously saved torrents
func (a *App) loadSavedTorrents() {
	data, err := os.ReadFile(a.stateFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Error reading torrent states: %v", err)
		}
		return
	}

	var states []TorrentState
	if err := json.Unmarshal(data, &states); err != nil {
		log.Printf("Error unmarshaling torrent states: %v", err)
		return
	}

	log.Printf("Loading %d saved torrents...", len(states))
	for _, state := range states {
		if state.MagnetURI != "" {
			t, err := a.client.AddMagnet(state.MagnetURI)
			if err != nil {
				log.Printf("Error re-adding torrent %s: %v", state.InfoHash, err)
				continue
			}

			hash := t.InfoHash().String()

			// Initialize trackers
			a.speedsMutex.Lock()
			a.downloadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
			a.uploadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
			a.speedsMutex.Unlock()

			a.torrentsMutex.Lock()
			a.torrents[hash] = t
			a.torrentsMutex.Unlock()

			// Restore paused state
			if state.IsPaused {
				a.pausedMutex.Lock()
				a.pausedTorrents[hash] = true
				a.pausedMutex.Unlock()
			} else {
				// Wait for info and start download
				go func(torr *torrent.Torrent) {
					<-torr.GotInfo()
					torr.DownloadAll()
				}(t)
			}

			log.Printf("✓ Restored torrent: %s (paused: %v)", hash, state.IsPaused)
		}
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

	// Initialize speed trackers
	a.speedsMutex.Lock()
	a.downloadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
	a.uploadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
	a.speedsMutex.Unlock()

	// Add to torrents map
	a.torrentsMutex.Lock()
	a.torrents[hash] = t
	a.torrentsMutex.Unlock()

	log.Printf("Added magnet link, waiting for metadata...")

	// Wait for info and start download
	go func() {
		select {
		case <-t.GotInfo():
			log.Printf("✓ Got metadata for torrent: %s", t.Name())
			t.DownloadAll()
			a.saveTorrentStates()
			wailsruntime.EventsEmit(a.ctx, "torrent-added", hash)
		case <-time.After(60 * time.Second):
			log.Printf("⚠ Timeout waiting for torrent metadata")
			wailsruntime.LogWarning(a.ctx, "Could not fetch torrent metadata within 60 seconds")
		}
	}()

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

	a.saveTorrentStates()

	log.Printf("✓ Added torrent file: %s", t.Name())
	wailsruntime.EventsEmit(a.ctx, "torrent-added", hash)

	return nil
}

// CreateTorrentFromFiles creates a torrent from local files and starts seeding
func (a *App) CreateTorrentFromFiles(files []string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("torrent client not initialized")
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no files provided")
	}

	log.Printf("Creating torrent from %d file(s)...", len(files))

	// Determine if single file or multiple files
	var rootPath string
	if len(files) == 1 {
		rootPath = files[0]
	} else {
		// For multiple files, use parent directory
		rootPath = filepath.Dir(files[0])
	}

	// Build metainfo
	info := metainfo.Info{
		PieceLength: 256 * 1024, // 256 KB pieces
	}

	// Build from file path
	if err := info.BuildFromFilePath(rootPath); err != nil {
		return "", fmt.Errorf("failed to build torrent info: %w", err)
	}

	log.Printf("Generating pieces for torrent...")

	// Generate pieces (hash the data)
	err := info.GeneratePieces(func(fi metainfo.FileInfo) (io.ReadCloser, error) {
		fullPath := filepath.Join(rootPath, filepath.Join(fi.Path...))
		return os.Open(fullPath)
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate pieces: %w", err)
	}

	log.Printf("Torrent info generated, size: %d bytes", info.TotalLength())

	// Create metainfo with trackers
	mi := metainfo.MetaInfo{
		AnnounceList: [][]string{
			{"udp://tracker.openbittorrent.com:6969/announce"},
			{"udp://tracker.opentrackr.org:1337/announce"},
			{"udp://tracker.pomf.se:80/announce"},
		},
		InfoBytes: bencode.MustMarshal(info),
	}
	mi.SetDefaults()

	// Add torrent to client for seeding
	t, err := a.client.AddTorrent(&mi)
	if err != nil {
		return "", fmt.Errorf("failed to add torrent for seeding: %w", err)
	}

	hash := t.InfoHash().String()
	log.Printf("Added torrent with hash: %s", hash)

	// Initialize speed trackers
	a.speedsMutex.Lock()
	a.downloadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
	a.uploadSpeeds[hash] = &speedTracker{lastTime: time.Now()}
	a.speedsMutex.Unlock()

	// Add to torrents map
	a.torrentsMutex.Lock()
	a.torrents[hash] = t
	a.torrentsMutex.Unlock()

	// Wait for info and verify existing files
	<-t.GotInfo()
	log.Printf("Got torrent info: %s", t.Name())

	// Download all pieces (this verifies existing data)
	t.Seeding()
	log.Printf("Started verification of existing files...")

	// Wait for verification to complete
	go func() {
		// Give it time to verify the files
		for i := 0; i < 30; i++ {
			time.Sleep(500 * time.Millisecond)

			// Check if verification is complete
			if t.BytesCompleted() >= t.Length() {
				log.Printf("✓ File verification complete: %d/%d bytes", t.BytesCompleted(), t.Length())
				break
			}

			if i == 29 {
				log.Printf("⚠ Verification taking longer than expected: %d/%d bytes verified", t.BytesCompleted(), t.Length())
			}
		}

		wailsruntime.EventsEmit(a.ctx, "torrent-added", hash)
	}()

	a.saveTorrentStates()

	// Get magnet link
	magnet, err := mi.MagnetV2()
	if err != nil {
		log.Printf("Warning: failed to generate magnet link: %v", err)
		return "", fmt.Errorf("failed to generate magnet link: %w", err)
	}

	magnetStr := magnet.String()
	log.Printf("✓ Created and seeding torrent: %s", t.Name())
	log.Printf("✓ Magnet link: %s", magnetStr)

	return magnetStr, nil
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

	// Cancel all pieces to stop downloading
	t.CancelPieces(0, t.NumPieces())

	// Optionally drop connections
	t.Drop()

	// Re-add immediately but don't start download
	if t.Info() != nil {
		mi := metainfo.MetaInfo{
			InfoBytes: bencode.MustMarshal(*t.Info()),
		}

		newT, err := a.client.AddTorrent(&mi)
		if err != nil {
			return fmt.Errorf("failed to re-add torrent: %w", err)
		}

		// Update reference
		a.torrentsMutex.Lock()
		a.torrents[infoHash] = newT
		a.torrentsMutex.Unlock()
	}

	// Mark as paused
	a.pausedMutex.Lock()
	a.pausedTorrents[infoHash] = true
	a.pausedMutex.Unlock()

	a.saveTorrentStates()

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

	// Start downloading all pieces
	t.DownloadAll()

	// Mark as not paused
	a.pausedMutex.Lock()
	delete(a.pausedTorrents, infoHash)
	a.pausedMutex.Unlock()

	a.saveTorrentStates()

	log.Printf("▶ Resumed torrent: %s", t.Name())
	return nil
}

// RemoveTorrent removes a torrent
func (a *App) RemoveTorrent(infoHash string, deleteFiles bool) error {
	log.Printf("🔍 RemoveTorrent called - InfoHash: %s, DeleteFiles: %t", infoHash, deleteFiles)

	a.torrentsMutex.Lock()
	t, exists := a.torrents[infoHash]
	if !exists {
		a.torrentsMutex.Unlock()
		log.Printf("❌ Torrent not found: %s", infoHash)
		return fmt.Errorf("torrent not found")
	}
	delete(a.torrents, infoHash)
	a.torrentsMutex.Unlock()

	log.Printf("✓ Torrent removed from map: %s", infoHash)

	torrentName := t.Name()
	if torrentName == "" {
		torrentName = infoHash
	}

	// Clean up speed trackers
	a.speedsMutex.Lock()
	delete(a.downloadSpeeds, infoHash)
	delete(a.uploadSpeeds, infoHash)
	a.speedsMutex.Unlock()

	// Clean up paused state
	a.pausedMutex.Lock()
	delete(a.pausedTorrents, infoHash)
	a.pausedMutex.Unlock()

	// Store file paths before dropping if we need to delete
	var filePaths []string
	if deleteFiles && t.Info() != nil {
		for _, file := range t.Files() {
			path := filepath.Join(a.downloadDir, file.Path())
			filePaths = append(filePaths, path)
			log.Printf("📁 File to delete: %s", path)
		}
	}

	// Drop torrent from client
	t.Drop()
	log.Printf("✓ Torrent dropped from client")

	// Delete files after dropping torrent
	if deleteFiles && len(filePaths) > 0 {
		for _, path := range filePaths {
			if err := os.Remove(path); err != nil {
				log.Printf("⚠️ Warning: failed to delete file %s: %v", path, err)
			} else {
				log.Printf("✓ Deleted file: %s", path)
			}
		}

		// Try to remove empty parent directories
		if len(filePaths) > 0 {
			parentDir := filepath.Dir(filePaths[0])
			if parentDir != a.downloadDir {
				if err := os.Remove(parentDir); err != nil {
					log.Printf("⚠️ Could not remove directory %s: %v", parentDir, err)
				}
			}
		}

		log.Printf("🗑 Removed torrent and deleted files: %s", torrentName)
	} else {
		log.Printf("🗑 Removed torrent: %s", torrentName)
	}

	a.saveTorrentStates() // Save state after removal
	log.Printf("✓ Torrent states saved")

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

	for hash, t := range a.torrents {
		stats := t.Stats()

		a.pausedMutex.RLock()
		isPaused := a.pausedTorrents[hash]
		a.pausedMutex.RUnlock()

		if !isPaused && t.BytesCompleted() < t.Length() {
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

// OpenDownloadFolder opens the download folder
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
	default:
		cmd = "xdg-open"
		args = []string{a.downloadDir}
	}

	if err := exec.Command(cmd, args...).Start(); err != nil {
		return fmt.Errorf("failed to open download folder: %w", err)
	}

	log.Printf("📁 Opened download folder: %s", a.downloadDir)
	return nil
}

// SelectTorrentFile opens file picker for torrent files
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

	return file, err
}

// SelectLocalFiles opens file picker for multiple files
func (a *App) SelectLocalFiles() ([]string, error) {
	files, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select Files to Share",
	})
	return files, err
}

// Helper functions

func (a *App) getTorrentInfo(hash string, t *torrent.Torrent) TorrentInfo {
	stats := t.Stats()

	// Check if paused
	a.pausedMutex.RLock()
	isPaused := a.pausedTorrents[hash]
	a.pausedMutex.RUnlock()

	// Determine status
	status := a.getTorrentStatus(t, stats, isPaused)

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

	// Get torrent name
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
		IsPaused:      isPaused,
	}
}

func (a *App) getTorrentStatus(t *torrent.Torrent, stats torrent.TorrentStats, isPaused bool) string {
	// First check if manually paused
	if isPaused {
		return "paused"
	}

	// Check if we have valid length info
	if t.Length() == 0 {
		return "loading"
	}

	// Check if download is complete (with small margin for rounding)
	bytesCompleted := t.BytesCompleted()
	totalLength := t.Length()

	if bytesCompleted >= totalLength {
		// Download is complete - we're seeding
		return "seeding"
	}

	// Still downloading
	progress := float64(bytesCompleted) / float64(totalLength)

	// If we have active download activity, we're downloading
	if stats.ActivePeers > 0 && progress < 1.0 {
		return "downloading"
	}

	// No active peers but we have discovered peers - stalled
	if stats.TotalPeers > 0 && progress < 1.0 {
		return "stalled"
	}

	// No peers at all and not complete - waiting for peers
	if progress < 1.0 {
		return "stalled"
	}

	return "seeding"
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

		// Get current state
		torrents := a.GetTorrents()
		stats := a.GetStats()

		data := map[string]interface{}{
			"torrents": torrents,
			"stats":    stats,
		}

		jsonData, _ := json.Marshal(data)
		dataStr := string(jsonData)

		// Check if data has meaningfully changed
		a.updateMutex.Lock()
		currentHash := fmt.Sprintf("%x", dataStr) // Simple hash
		timeSinceLastUpdate := time.Since(a.lastUpdateTime)

		// Only emit if data changed OR if it's been more than 5 seconds (for speed updates)
		if currentHash != a.lastUpdateHash || timeSinceLastUpdate > 5*time.Second {
			a.lastUpdateHash = currentHash
			a.lastUpdateTime = time.Now()
			a.updateMutex.Unlock()

			wailsruntime.EventsEmit(a.ctx, "torrents-update", dataStr)
		} else {
			a.updateMutex.Unlock()
		}
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

// Wallet functions
func (a *App) SetDepositAddress(address string) error {
	a.depositAddress = address
	return nil
}

func (a *App) GetDepositAddress() (string, error) {
	return a.depositAddress, nil
}

func (a *App) GetBalance() (float64, error) {
	return 0.0, nil
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "SeedRush - Earn while you seed",
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
