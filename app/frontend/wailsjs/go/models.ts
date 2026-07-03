export namespace main {
	
	export class AssetCard {
	    name: string;
	    description: string;
	    type: string;
	    typeLabel: string;
	    version: string;
	    versions: number;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new AssetCard(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.type = source["type"];
	        this.typeLabel = source["typeLabel"];
	        this.version = source["version"];
	        this.versions = source["versions"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class AssetFile {
	    path: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new AssetFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.content = source["content"];
	    }
	}
	export class AssetDetail {
	    name: string;
	    description: string;
	    type: string;
	    typeLabel: string;
	    version: string;
	    versions: string[];
	    files: AssetFile[];
	
	    static createFrom(source: any = {}) {
	        return new AssetDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.type = source["type"];
	        this.typeLabel = source["typeLabel"];
	        this.version = source["version"];
	        this.versions = source["versions"];
	        this.files = this.convertValues(source["files"], AssetFile);
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
	
	export class Draft {
	    id: string;
	    name: string;
	    type: string;
	    typeLabel: string;
	    description: string;
	    targetAsset: string;
	    files: AssetFile[];
	
	    static createFrom(source: any = {}) {
	        return new Draft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.typeLabel = source["typeLabel"];
	        this.description = source["description"];
	        this.targetAsset = source["targetAsset"];
	        this.files = this.convertValues(source["files"], AssetFile);
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
	export class VaultInfo {
	    configured: boolean;
	    type: string;
	    location: string;
	
	    static createFrom(source: any = {}) {
	        return new VaultInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.type = source["type"];
	        this.location = source["location"];
	    }
	}

}

