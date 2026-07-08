export namespace main {
	
	export class AIClient {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new AIClient(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
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
	
	export class AssetInstallation {
	    kind: string;
	    repo?: string;
	    paths?: string[];
	    team?: string;
	    user?: string;
	    bot?: string;
	    entityId?: string;
	    monoRepoConfigId?: string;
	
	    static createFrom(source: any = {}) {
	        return new AssetInstallation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.repo = source["repo"];
	        this.paths = source["paths"];
	        this.team = source["team"];
	        this.user = source["user"];
	        this.bot = source["bot"];
	        this.entityId = source["entityId"];
	        this.monoRepoConfigId = source["monoRepoConfigId"];
	    }
	}
	export class Collection {
	    name: string;
	    description: string;
	    assets: string[];
	
	    static createFrom(source: any = {}) {
	        return new Collection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.assets = source["assets"];
	    }
	}
	export class ContentMatch {
	    name: string;
	    matches: number;
	    before: string;
	    match: string;
	    after: string;
	
	    static createFrom(source: any = {}) {
	        return new ContentMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.matches = source["matches"];
	        this.before = source["before"];
	        this.match = source["match"];
	        this.after = source["after"];
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
	export class ExtensionScope {
	    shared: boolean;
	    personal: boolean;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new ExtensionScope(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.shared = source["shared"];
	        this.personal = source["personal"];
	        this.label = source["label"];
	    }
	}
	export class GitRepoOption {
	    name: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new GitRepoOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.url = source["url"];
	    }
	}
	export class GitStatusInfo {
	    available: boolean;
	    version: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new GitStatusInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.version = source["version"];
	        this.reason = source["reason"];
	    }
	}
	export class ImportResult {
	    created: string[];
	    skipped: number;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.created = source["created"];
	        this.skipped = source["skipped"];
	    }
	}
	export class InstallationsView {
	    everyone: boolean;
	    installations: AssetInstallation[];
	
	    static createFrom(source: any = {}) {
	        return new InstallationsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.everyone = source["everyone"];
	        this.installations = this.convertValues(source["installations"], AssetInstallation);
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
	export class InstalledAssetInfo {
	    name: string;
	    version: string;
	    scopes: string[];
	
	    static createFrom(source: any = {}) {
	        return new InstalledAssetInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.scopes = source["scopes"];
	    }
	}
	export class LibraryRemoval {
	    name: string;
	    type: string;
	    location: string;
	    lastLibrary: boolean;
	    active: boolean;
	    canDeleteSource: boolean;
	    sourceLabel: string;
	    sharedSource: boolean;
	    sourceProvider: string;
	
	    static createFrom(source: any = {}) {
	        return new LibraryRemoval(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.location = source["location"];
	        this.lastLibrary = source["lastLibrary"];
	        this.active = source["active"];
	        this.canDeleteSource = source["canDeleteSource"];
	        this.sourceLabel = source["sourceLabel"];
	        this.sharedSource = source["sharedSource"];
	        this.sourceProvider = source["sourceProvider"];
	    }
	}
	export class MarketplaceExtension {
	    assetName: string;
	    id: string;
	    name: string;
	    version: string;
	    description: string;
	    author: string;
	    permissions: string[];
	    installs: number;
	
	    static createFrom(source: any = {}) {
	        return new MarketplaceExtension(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.assetName = source["assetName"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.description = source["description"];
	        this.author = source["author"];
	        this.permissions = source["permissions"];
	        this.installs = source["installs"];
	    }
	}
	export class PluginAuditEventRecord {
	    timestamp: string;
	    actor: string;
	    event: string;
	    targetType: string;
	    target: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginAuditEventRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.actor = source["actor"];
	        this.event = source["event"];
	        this.targetType = source["targetType"];
	        this.target = source["target"];
	    }
	}
	export class PluginMetadataPatch {
	    description?: string;
	    keywords: string[];
	    owner?: string;
	    status?: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginMetadataPatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.description = source["description"];
	        this.keywords = source["keywords"];
	        this.owner = source["owner"];
	        this.status = source["status"];
	    }
	}
	export class PluginPolicy {
	    mode: string;
	    allowed: string[];
	
	    static createFrom(source: any = {}) {
	        return new PluginPolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.allowed = source["allowed"];
	    }
	}
	export class PluginTeamRecord {
	    name: string;
	    members: string[];
	
	    static createFrom(source: any = {}) {
	        return new PluginTeamRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.members = source["members"];
	    }
	}
	export class PluginUsageEventRecord {
	    timestamp: string;
	    actor: string;
	    assetName: string;
	    assetVersion: string;
	    assetType: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginUsageEventRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.actor = source["actor"];
	        this.assetName = source["assetName"];
	        this.assetVersion = source["assetVersion"];
	        this.assetType = source["assetType"];
	    }
	}
	export class PluginUserActivity {
	    actor: string;
	    events: number;
	    distinctAssets: number;
	
	    static createFrom(source: any = {}) {
	        return new PluginUserActivity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.actor = source["actor"];
	        this.events = source["events"];
	        this.distinctAssets = source["distinctAssets"];
	    }
	}
	export class PluginUserStatsResult {
	    knownUsers: string[];
	    active: PluginUserActivity[];
	
	    static createFrom(source: any = {}) {
	        return new PluginUserStatsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.knownUsers = source["knownUsers"];
	        this.active = this.convertValues(source["active"], PluginUserActivity);
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
	export class ProfileInfo {
	    name: string;
	    type: string;
	    location: string;
	    identity: string;
	    default: boolean;
	    active: boolean;
	    trackRepos: boolean;
	    icon: string;
	
	    static createFrom(source: any = {}) {
	        return new ProfileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.location = source["location"];
	        this.identity = source["identity"];
	        this.default = source["default"];
	        this.active = source["active"];
	        this.trackRepos = source["trackRepos"];
	        this.icon = source["icon"];
	    }
	}
	export class Settings {
	    profiles: ProfileInfo[];
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profiles = this.convertValues(source["profiles"], ProfileInfo);
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
	export class SleuthLoginStart {
	    verificationUri: string;
	    userCode: string;
	    deviceCode: string;
	    browserOpened: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SleuthLoginStart(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.verificationUri = source["verificationUri"];
	        this.userCode = source["userCode"];
	        this.deviceCode = source["deviceCode"];
	        this.browserOpened = source["browserOpened"];
	    }
	}
	export class SyncFolderOption {
	    provider: string;
	    path: string;
	    suggested: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncFolderOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.path = source["path"];
	        this.suggested = source["suggested"];
	    }
	}
	export class TeamInfo {
	    name: string;
	    description: string;
	    members: string[];
	    admins: string[];
	    repositories: string[];
	
	    static createFrom(source: any = {}) {
	        return new TeamInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.members = source["members"];
	        this.admins = source["admins"];
	        this.repositories = source["repositories"];
	    }
	}
	export class UpdateInfo {
	    available: boolean;
	    version: string;
	    url: string;
	    installed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.version = source["version"];
	        this.url = source["url"];
	        this.installed = source["installed"];
	    }
	}
	export class VaultInfo {
	    configured: boolean;
	    type: string;
	    location: string;
	    name: string;
	    identity: string;
	    trackRepos: boolean;
	    icon: string;
	
	    static createFrom(source: any = {}) {
	        return new VaultInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.type = source["type"];
	        this.location = source["location"];
	        this.name = source["name"];
	        this.identity = source["identity"];
	        this.trackRepos = source["trackRepos"];
	        this.icon = source["icon"];
	    }
	}
	export class VaultPlugin {
	    assetName: string;
	    manifest: string;
	    source: string;
	    scope: ExtensionScope;
	
	    static createFrom(source: any = {}) {
	        return new VaultPlugin(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.assetName = source["assetName"];
	        this.manifest = source["manifest"];
	        this.source = source["source"];
	        this.scope = this.convertValues(source["scope"], ExtensionScope);
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

