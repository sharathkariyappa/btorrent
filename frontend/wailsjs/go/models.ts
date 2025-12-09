export namespace main {
	
	export class FileInfo {
	    name: string;
	    size: number;
	    sizeStr: string;
	    progress: number;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	        this.sizeStr = source["sizeStr"];
	        this.progress = source["progress"];
	        this.path = source["path"];
	    }
	}
	export class Stats {
	    totalDownload: string;
	    totalUpload: string;
	    activeTorrents: number;
	    totalPeers: number;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalDownload = source["totalDownload"];
	        this.totalUpload = source["totalUpload"];
	        this.activeTorrents = source["activeTorrents"];
	        this.totalPeers = source["totalPeers"];
	    }
	}
	export class TorrentInfo {
	    id: string;
	    name: string;
	    infoHash: string;
	    size: number;
	    sizeStr: string;
	    progress: number;
	    status: string;
	    downloadSpeed: number;
	    uploadSpeed: number;
	    downloadSpeedStr: string;
	    uploadSpeedStr: string;
	    peers: number;
	    seeds: number;
	    eta: string;
	    files: FileInfo[];
	    // Go type: time
	    addedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new TorrentInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.infoHash = source["infoHash"];
	        this.size = source["size"];
	        this.sizeStr = source["sizeStr"];
	        this.progress = source["progress"];
	        this.status = source["status"];
	        this.downloadSpeed = source["downloadSpeed"];
	        this.uploadSpeed = source["uploadSpeed"];
	        this.downloadSpeedStr = source["downloadSpeedStr"];
	        this.uploadSpeedStr = source["uploadSpeedStr"];
	        this.peers = source["peers"];
	        this.seeds = source["seeds"];
	        this.eta = source["eta"];
	        this.files = this.convertValues(source["files"], FileInfo);
	        this.addedAt = this.convertValues(source["addedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

