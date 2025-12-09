import React, { useState, useEffect } from 'react';
import { Play, Pause, Trash2, Plus, Download, Upload, Users, Settings, FolderOpen, Link, Search, X, FileUp, Clock, HardDrive, Wallet, DollarSign, Check, AlertCircle, Copy } from 'lucide-react';
import { AddMagnet, AddTorrentFile, GetTorrents, GetStats, PauseTorrent, ResumeTorrent, RemoveTorrent, OpenDownloadFolder, SelectTorrentFile, SelectLocalFiles, GetBalance, SetDepositAddress, GetDepositAddress } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

const TorrentClient = () => {
  const [torrents, setTorrents] = useState([]);
  const [stats, setStats] = useState({
    totalDownload: '0 B/s',
    totalUpload: '0 B/s',
    activeTorrents: 0,
    totalPeers: 0
  });
  const [selectedTorrent, setSelectedTorrent] = useState(null);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showPaymentModal, setShowPaymentModal] = useState(false);
  const [showDepositModal, setShowDepositModal] = useState(false);
  const [showBalanceModal, setShowBalanceModal] = useState(false);
  const [showLocalFilesModal, setShowLocalFilesModal] = useState(false);
  const [magnetLink, setMagnetLink] = useState('');
  const [filterStatus, setFilterStatus] = useState('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [successMessage, setSuccessMessage] = useState('');
  const [paymentAmount, setPaymentAmount] = useState('');
  const [walletBalance, setWalletBalance] = useState('0.00000000');
  const [selectedLocalFiles, setSelectedLocalFiles] = useState([]);
  const [localFilePrice, setLocalFilePrice] = useState('');
  const [pendingTorrent, setPendingTorrent] = useState(null);

  // Load torrents on mount
  useEffect(() => {
    loadTorrents();
    loadDepositAddress();

    // Listen for real-time updates
    const unsubscribeUpdate = EventsOn('torrents-update', (data) => {
      try {
        const parsed = JSON.parse(data);
        setTorrents(parsed.torrents || []);
        setStats(parsed.stats || stats);
      } catch (e) {
        console.error('Failed to parse update:', e);
      }
    });

    const unsubscribeAdded = EventsOn('torrent-added', () => {
      loadTorrents();
      setSuccessMessage('Torrent added successfully!');
      setTimeout(() => setSuccessMessage(''), 3000);
    });

    // Cleanup event listeners
    return () => {
      if (unsubscribeUpdate) unsubscribeUpdate();
      if (unsubscribeAdded) unsubscribeAdded();
    };
  }, []);

  const loadTorrents = async () => {
    try {
      const result = await GetTorrents();
      setTorrents(result || []);
      const statsResult = await GetStats();
      setStats(statsResult);
    } catch (err) {
      console.error('Failed to load torrents:', err);
    }
  };

  const loadDepositAddress = async () => {
    try {
      const address = await GetDepositAddress();
      setCurrentDepositAddress(address || '');
    } catch (err) {
      console.error('Failed to load deposit address:', err);
    }
  };

  const handleViewBalance = async () => {
    try {
      const balance = await GetBalance();
      setWalletBalance(balance);
      setShowBalanceModal(true);
    } catch (err) {
      setError('Failed to retrieve wallet balance');
      setTimeout(() => setError(''), 3000);
    }
  };

  const handleAddMagnet = async () => {
    if (!magnetLink.trim()) {
      setError('Please enter a magnet link');
      return;
    }

    if (!magnetLink.startsWith('magnet:?')) {
      setError('Invalid magnet link format');
      return;
    }

    setLoading(true);
    setError('');

    try {
      await AddMagnet(magnetLink);
      setMagnetLink('');
      setShowAddModal(false);
      setSuccessMessage('Fetching torrent metadata...');
      setTimeout(() => setSuccessMessage(''), 3000);
    } catch (err) {
      setError(err.message || 'Failed to add torrent');
    } finally {
      setLoading(false);
    }
  };

  const handleAddTorrentFile = async () => {
    
    setLoading(true);
    setError('');

    try {
      const filePath = await SelectTorrentFile();
      if (filePath) {
        await AddTorrentFile(filePath);
        setShowAddModal(false);
        setSuccessMessage('Torrent file added!');
        setTimeout(() => setSuccessMessage(''), 3000);
      }
    } catch (err) {
      setError(err.message || 'Failed to add torrent file');
    } finally {
      setLoading(false);
    }
  };

  const handleSelectLocalFiles = async () => {

    try {
      const files = await SelectLocalFiles();
      if (files && files.length > 0) {
        setSelectedLocalFiles(files);
        setShowLocalFilesModal(true);
      }
    } catch (err) {
      setError('Failed to select files');
      setTimeout(() => setError(''), 3000);
    }
  };

  const handleShareLocalFiles = async () => {
    if (!localFilePrice || parseFloat(localFilePrice) < 0) {
      setError('Please enter a valid price (0 for free)');
      return;
    }

    setLoading(true);
    try {
      // Here you would create a torrent from local files and add it
      // For now, we'll simulate the process
      setSuccessMessage('Creating torrent from local files...');
      setShowLocalFilesModal(false);
      setSelectedLocalFiles([]);
      setLocalFilePrice('');
      setTimeout(() => {
        setSuccessMessage('Files shared successfully!');
        setTimeout(() => setSuccessMessage(''), 3000);
      }, 2000);
    } catch (err) {
      setError('Failed to share local files');
      setTimeout(() => setError(''), 3000);
    } finally {
      setLoading(false);
    }
  };

  const handleDownloadTorrent = (torrent) => {
    // Check if torrent requires payment
    if (torrent.price && parseFloat(torrent.price) > 0) {
      setPendingTorrent(torrent);
      setPaymentAmount(torrent.price);
      setShowPaymentModal(true);
    } else {
      // Free torrent, start download immediately
      handleToggleStatus(torrent);
    }
  };

  const handleConfirmPayment = () => {
    if (!paymentAmount || parseFloat(paymentAmount) <= 0) {
      setError('Please enter a valid payment amount');
      return;
    }

    // Redirect to BSV wallet with payment details
    const paymentUrl = `bsv://pay?address=${pendingTorrent.seederAddress}&amount=${paymentAmount}&label=Torrent:${encodeURIComponent(pendingTorrent.name)}`;
    
    setSuccessMessage('Redirecting to BSV wallet...');
    window.location.href = paymentUrl;
    
    // Close modal after redirect
    setTimeout(() => {
      setShowPaymentModal(false);
      setPendingTorrent(null);
      setPaymentAmount('');
    }, 1000);
  };

  const handleToggleStatus = async (torrent) => {
    try {
      if (torrent.status === 'paused' || torrent.status === 'stalled') {
        await ResumeTorrent(torrent.infoHash);
      } else {
        await PauseTorrent(torrent.infoHash);
      }
      await loadTorrents();
    } catch (err) {
      console.error('Failed to toggle torrent:', err);
      setError('Failed to change torrent status');
      setTimeout(() => setError(''), 3000);
    }
  };

  const handleRemoveTorrent = async (torrent, deleteFiles = false) => {
    const message = deleteFiles 
      ? `Remove "${torrent.name}" and delete downloaded files?`
      : `Remove "${torrent.name}"?`;
      
    if (window.confirm(message)) {
      try {
        await RemoveTorrent(torrent.infoHash, deleteFiles);
        if (selectedTorrent?.infoHash === torrent.infoHash) {
          setSelectedTorrent(null);
        }
        await loadTorrents();
        setSuccessMessage(deleteFiles ? 'Torrent and files removed' : 'Torrent removed');
        setTimeout(() => setSuccessMessage(''), 3000);
      } catch (err) {
        console.error('Failed to remove torrent:', err);
        setError('Failed to remove torrent');
        setTimeout(() => setError(''), 3000);
      }
    }
  };

  const handleOpenFolder = async () => {
    try {
      await OpenDownloadFolder();
    } catch (err) {
      console.error('Failed to open folder:', err);
      setError('Failed to open download folder');
      setTimeout(() => setError(''), 3000);
    }
  };

  const copyToClipboard = (text) => {
    navigator.clipboard.writeText(text);
    setSuccessMessage('Copied to clipboard!');
    setTimeout(() => setSuccessMessage(''), 2000);
  };

  const filteredTorrents = torrents.filter(t => {
    const matchesStatus = filterStatus === 'all' || t.status === filterStatus;
    const matchesSearch = t.name.toLowerCase().includes(searchQuery.toLowerCase());
    return matchesStatus && matchesSearch;
  });

  const getStatusColor = (status) => {
    switch(status) {
      case 'downloading':
        return 'bg-[#06E7ED]/20 text-[#06E7ED]';
      case 'seeding':
        return 'bg-green-500/20 text-green-300';
      case 'completed':
        return 'bg-blue-500/20 text-blue-300';
      case 'stalled':
        return 'bg-yellow-500/20 text-yellow-300';
      case 'paused':
        return 'bg-gray-500/20 text-gray-300';
      default:
        return 'bg-gray-500/20 text-gray-300';
    }
  };

  return (
    <div className="h-screen flex flex-col bg-[#081B2A] text-white">
      {/* Toast Notifications */}
      {(successMessage || error) && (
        <div className={`fixed top-4 right-4 z-50 px-4 py-3 rounded-lg shadow-lg ${
          error ? 'bg-red-500/90' : 'bg-green-500/90'
        } backdrop-blur-sm transition-all`}>
          <p className="text-sm font-medium text-white">
            {error || successMessage}
          </p>
        </div>
      )}

      {/* Top Bar */}
      <div className="bg-[#0E1F2D] px-6 py-4 border-b border-white/5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg bg-[#06E7ED]/10 flex items-center justify-center">
              <img src="/Frame 1194.svg" alt="Logo" className="w-10 h-10" />
            </div>
            <div>
              <h1 className="text-xl font-bold text-white">Btorrent</h1>
              <p className="text-xs text-gray-400">Modern Torrent Client</p>
            </div>
          </div>

          <div className="flex items-center gap-4">
            <div className="flex items-center gap-4 px-4 py-2 bg-[#081B2A]/50 rounded-lg border border-white/5">
              <div className="flex items-center gap-2">
                <Download className="w-4 h-4 text-[#06E7ED]" />
                <span className="text-sm font-medium">{stats.totalDownload}</span>
              </div>
              <div className="w-px h-4 bg-white/10"></div>
              <div className="flex items-center gap-2">
                <Upload className="w-4 h-4 text-[#06E7ED]" />
                <span className="text-sm font-medium">{stats.totalUpload}</span>
              </div>
            </div>

            <button 
              onClick={handleViewBalance}
              className="px-4 py-2 bg-[#06E7ED]/10 hover:bg-[#06E7ED]/20 text-[#06E7ED] rounded-lg transition-all flex items-center gap-2 text-sm font-medium border border-[#06E7ED]/20"
              title="View Balance"
            >
              <Wallet className="w-4 h-4" />
              Balance
            </button>

            <button 
              onClick={handleOpenFolder}
              className="p-2 hover:bg-white/10 rounded-lg transition-all"
              title="Open Downloads Folder"
            >
              <FolderOpen className="w-5 h-5" />
            </button>

            <button 
            >
              <Settings className="w-5 h-5" />
            </button>
          </div>
        </div>
      </div>

      {/* Main Content */}
      <div className="flex-1 flex overflow-hidden">
        {/* Sidebar */}
        <div className="w-64 bg-[#0C2437] p-4 border-r border-white/5">
          <button
            onClick={() => setShowAddModal(true)}
            className="w-full bg-[#06E7ED] hover:bg-[#05CDD3] text-[#081B2A] rounded-lg px-4 py-3 flex items-center justify-center gap-2 font-semibold transition-all shadow-lg shadow-cyan-500/20 mb-3"
          >
            <Plus className="w-5 h-5" />
            Add Torrent
          </button>

          <button
            onClick={handleSelectLocalFiles}
            className="w-full bg-[#0E1F2D] hover:bg-white/5 text-white rounded-lg px-4 py-3 flex items-center justify-center gap-2 font-semibold transition-all border border-white/10 mb-6"
          >
            <FileUp className="w-5 h-5" />
            Share Local Files
          </button>

          <div className="space-y-2">
            <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider px-3 mb-3">
              Filters
            </h3>
            {[
              { key: 'all', label: 'All' },
              { key: 'downloading', label: 'Downloading' },
              { key: 'seeding', label: 'Seeding' },
              { key: 'completed', label: 'Completed' },
              { key: 'paused', label: 'Paused' },
              { key: 'stalled', label: 'Stalled' }
            ].map(({ key, label }) => (
              <button
                key={key}
                onClick={() => setFilterStatus(key)}
                className={`w-full px-3 py-2 rounded-lg text-left text-sm transition-all ${
                  filterStatus === key
                    ? 'bg-[#06E7ED]/10 text-[#06E7ED]'
                    : 'hover:bg-white/5 text-gray-300'
                }`}
              >
                <span>{label}</span>
                <span className="float-right text-xs text-gray-500">
                  {key === 'all' ? torrents.length : torrents.filter(t => t.status === key).length}
                </span>
              </button>
            ))}
          </div>

          <div className="mt-8 p-4 bg-[#0E1F2D] rounded-lg border border-white/5">
            <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-3">
              Statistics
            </h3>
            <div className="space-y-2 text-sm">
              <div className="flex justify-between">
                <span className="text-gray-400">Active</span>
                <span className="font-medium text-[#06E7ED]">{stats.activeTorrents}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-gray-400">Total Peers</span>
                <span className="font-medium text-[#06E7ED]">{stats.totalPeers}</span>
              </div>
            </div>
          </div>
        </div>

        {/* Torrent List */}
        <div className="flex-1 flex flex-col">
          {/* Search Bar */}
          <div className="p-4 bg-[#0E1F2D] border-b border-white/5">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-gray-400" />
              <input
                type="text"
                placeholder="Search torrents..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="w-full bg-[#081B2A]/50 border border-white/5 rounded-lg pl-10 pr-4 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-[#06E7ED] focus:border-transparent transition-all"
              />
              {searchQuery && (
                <button
                  onClick={() => setSearchQuery('')}
                  className="absolute right-3 top-1/2 -translate-y-1/2 p-1 hover:bg-white/10 rounded"
                >
                  <X className="w-4 h-4 text-gray-400" />
                </button>
              )}
            </div>
          </div>

          {/* Torrent Items */}
          <div className="flex-1 overflow-y-auto p-4 space-y-3">
            {filteredTorrents.length === 0 ? (
              <div className="flex flex-col items-center justify-center h-full text-gray-400">
                <Download className="w-16 h-16 mb-4 opacity-20" />
                <p className="text-lg font-medium">
                  {searchQuery ? 'No torrents found' : 'No torrents yet'}
                </p>
                <p className="text-sm">
                  {searchQuery ? 'Try a different search' : 'Add a torrent to get started'}
                </p>
              </div>
            ) : (
              filteredTorrents.map(torrent => (
                <div
                  key={torrent.id}
                  onClick={() => setSelectedTorrent(torrent)}
                  className={`bg-[#0E1F2D] rounded-xl p-4 transition-all cursor-pointer border ${
                    selectedTorrent?.id === torrent.id
                      ? 'ring-2 ring-[#06E7ED] shadow-lg shadow-cyan-500/20 border-[#06E7ED]'
                      : 'border-white/5 hover:border-white/10'
                  }`}
                >
                  <div className="flex items-start justify-between mb-3">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-1">
                        <h3 className="font-semibold text-white truncate">{torrent.name}</h3>
                        {torrent.price && parseFloat(torrent.price) > 0 && (
                          <span className="px-2 py-0.5 bg-green-500/20 text-green-300 rounded text-xs font-medium flex items-center gap-1">
                            <DollarSign className="w-3 h-3" />
                            {torrent.price} BSV
                          </span>
                        )}
                      </div>
                      <div className="flex items-center gap-3 mt-1 text-xs text-gray-400">
                        <span className="flex items-center gap-1">
                          <HardDrive className="w-3 h-3" />
                          {torrent.sizeStr}
                        </span>
                        <span>•</span>
                        <span className="flex items-center gap-1">
                          <Users className="w-3 h-3" />
                          {torrent.peers}
                        </span>
                        {torrent.eta && torrent.eta !== 'Unknown' && (
                          <>
                            <span>•</span>
                            <span className="flex items-center gap-1">
                              <Clock className="w-3 h-3" />
                              {torrent.eta}
                            </span>
                          </>
                        )}
                        <span>•</span>
                        <span className={`px-2 py-0.5 rounded ${getStatusColor(torrent.status)}`}>
                          {torrent.status}
                        </span>
                      </div>
                    </div>
                    <div className="flex items-center gap-2 ml-4">
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          handleDownloadTorrent(torrent);
                        }}
                        className="p-2 hover:bg-white/10 rounded-lg transition-all"
                        title={torrent.status === 'paused' || torrent.status === 'stalled' ? 'Resume' : 'Pause'}
                      >
                        {torrent.status === 'paused' || torrent.status === 'stalled' ? (
                          <Play className="w-4 h-4 text-[#06E7ED]" />
                        ) : (
                          <Pause className="w-4 h-4 text-orange-400" />
                        )}
                      </button>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          handleRemoveTorrent(torrent, false);
                        }}
                        className="p-2 hover:bg-red-500/20 rounded-lg transition-all"
                        title="Remove"
                      >
                        <Trash2 className="w-4 h-4 text-red-400" />
                      </button>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <div className="flex items-center justify-between text-xs text-gray-400">
                      <span className="font-medium">{torrent.progress.toFixed(1)}%</span>
                    </div>
                    <div className="h-1.5 bg-white/10 rounded-full overflow-hidden">
                      <div
                        className="h-full bg-gradient-to-r from-[#06E7ED] to-[#05CDD3] rounded-full transition-all duration-500"
                        style={{ width: `${Math.min(torrent.progress, 100)}%` }}
                      />
                    </div>
                    <div className="flex items-center justify-between text-xs">
                      <span className="text-[#06E7ED] flex items-center gap-1">
                        <Download className="w-3 h-3" />
                        {torrent.downloadSpeedStr}
                      </span>
                      <span className="text-green-400 flex items-center gap-1">
                        <Upload className="w-3 h-3" />
                        {torrent.uploadSpeedStr}
                      </span>
                    </div>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>

        {/* Details Panel */}
        {selectedTorrent && (
          <div className="w-96 bg-[#0C2437] p-6 overflow-y-auto border-l border-white/5">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-lg font-bold">Details</h2>
              <button
                onClick={() => setSelectedTorrent(null)}
                className="p-1 hover:bg-white/10 rounded transition-all"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="space-y-6">
              {selectedTorrent.price && parseFloat(selectedTorrent.price) > 0 && (
                <div className="bg-green-500/10 border border-green-500/20 rounded-lg p-4">
                  <div className="flex items-center gap-2 mb-2">
                    <DollarSign className="w-5 h-5 text-green-400" />
                    <span className="font-semibold text-green-300">Paid Content</span>
                  </div>
                  <p className="text-sm text-gray-300">
                    Price: <span className="font-bold text-green-400">{selectedTorrent.price} BSV</span>
                  </p>
                </div>
              )}

              <div>
                <h3 className="text-sm font-semibold text-gray-400 mb-3">FILES</h3>
                <div className="space-y-2">
                  {selectedTorrent.files && selectedTorrent.files.length > 0 ? (
                    selectedTorrent.files.map((file, idx) => (
                      <div key={idx} className="bg-[#0E1F2D] rounded-lg p-3 border border-white/5">
                        <div className="flex items-center justify-between mb-2">
                          <span className="text-sm font-medium truncate flex-1" title={file.name}>
                            {file.name}
                          </span>
                          <span className="text-xs text-gray-400 ml-2">{file.sizeStr}</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <div className="flex-1 h-1 bg-white/10 rounded-full overflow-hidden">
                            <div
                              className="h-full bg-[#06E7ED]"
                              style={{ width: `${Math.min(file.progress, 100)}%` }}
                            />
                          </div>
                          <span className="text-xs text-gray-500 min-w-[45px] text-right">
                            {file.progress.toFixed(0)}%
                          </span>
                        </div>
                      </div>
                    ))
                  ) : (
                    <div className="text-sm text-gray-400 bg-[#0E1F2D] rounded-lg p-4 text-center border border-white/5">
                      {selectedTorrent.name === 'Loading metadata...' 
                        ? 'Waiting for metadata...' 
                        : 'No file information available'}
                    </div>
                  )}
                </div>
              </div>

              <div>
                <h3 className="text-sm font-semibold text-gray-400 mb-3">INFORMATION</h3>
                <div className="space-y-3 text-sm bg-[#0E1F2D] rounded-lg p-4 border border-white/5">
                  <div className="flex justify-between">
                    <span className="text-gray-400">Status</span>
                    <span className={`font-medium capitalize px-2 py-0.5 rounded text-xs ${getStatusColor(selectedTorrent.status)}`}>
                      {selectedTorrent.status}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-400">Size</span>
                    <span className="font-medium">{selectedTorrent.sizeStr}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-400">Progress</span>
                    <span className="font-medium text-[#06E7ED]">
                      {selectedTorrent.progress.toFixed(1)}%
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-400">Download</span>
                    <span className="font-medium text-[#06E7ED]">
                      {selectedTorrent.downloadSpeedStr}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-400">Upload</span>
                    <span className="font-medium text-green-400">
                      {selectedTorrent.uploadSpeedStr}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-400">Peers</span>
                    <span className="font-medium">{selectedTorrent.peers}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-400">Seeds</span>
                    <span className="font-medium">{selectedTorrent.seeds}</span>
                  </div>
                  {selectedTorrent.eta && selectedTorrent.eta !== 'Unknown' && (
                    <div className="flex justify-between">
                      <span className="text-gray-400">ETA</span>
                      <span className="font-medium">{selectedTorrent.eta}</span>
                    </div>
                  )}
                </div>
              </div>

              <div className="space-y-2">
                <button 
                  onClick={handleOpenFolder}
                  className="w-full bg-[#06E7ED] hover:bg-[#05CDD3] text-[#081B2A] rounded-lg py-2.5 text-sm font-semibold transition-all flex items-center justify-center gap-2"
                >
                  <FolderOpen className="w-4 h-4" />
                  Open Download Folder
                </button>
                <button 
                  onClick={() => handleRemoveTorrent(selectedTorrent, true)}
                  className="w-full bg-red-500/10 hover:bg-red-500/20 text-red-400 rounded-lg py-2.5 text-sm font-semibold transition-all flex items-center justify-center gap-2 border border-red-500/20"
                >
                  <Trash2 className="w-4 h-4" />
                  Remove & Delete Files
                </button>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Add Torrent Modal */}
      {showAddModal && (
        <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div className="bg-[#0C2437] rounded-2xl p-6 w-full max-w-lg shadow-2xl border border-white/10">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-xl font-bold">Add Torrent</h2>
              <button
                onClick={() => {
                  setShowAddModal(false);
                  setError('');
                  setMagnetLink('');
                }}
                className="p-2 hover:bg-white/10 rounded-lg transition-all"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            {error && (
              <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm flex items-start gap-2">
                <X className="w-4 h-4 mt-0.5 flex-shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <div className="space-y-4">
              <div>
                <label className="text-sm font-medium text-gray-300 mb-2 block">
                  Magnet Link
                </label>
                <div className="relative">
                  <Link className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-gray-400" />
                  <input
                    type="text"
                    placeholder="magnet:?xt=urn:btih:..."
                    value={magnetLink}
                    onChange={(e) => setMagnetLink(e.target.value)}
                    onKeyPress={(e) => {
                      if (e.key === 'Enter' && magnetLink.trim()) {
                        handleAddMagnet();
                      }
                    }}
                    className="w-full bg-[#0E1F2D] border border-white/5 rounded-lg pl-10 pr-4 py-3 focus:outline-none focus:ring-2 focus:ring-[#06E7ED] focus:border-transparent transition-all"
                    disabled={loading}
                  />
                </div>
                <p className="text-xs text-gray-500 mt-2">
                  Press Enter to add
                </p>
              </div>

              <div className="flex gap-3">
                <button
                  onClick={handleAddMagnet}
                  disabled={loading || !magnetLink.trim()}
                  className="flex-1 bg-[#06E7ED] hover:bg-[#05CDD3] text-[#081B2A] rounded-lg py-3 font-semibold transition-all shadow-lg shadow-cyan-500/20 disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {loading ? 'Adding...' : 'Add Magnet'}
                </button>
                <button
                  onClick={handleAddTorrentFile}
                  disabled={loading}
                  className="px-6 bg-[#0E1F2D] hover:bg-white/5 border border-white/10 rounded-lg font-medium transition-all flex items-center gap-2 disabled:opacity-50"
                >
                  <FileUp className="w-4 h-4" />
                  File
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default TorrentClient;