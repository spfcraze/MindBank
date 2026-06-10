package repository

import (
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// normalizeUnicode strips accents and normalizes to ASCII for FTS compatibility.
// "café" → "cafe", "résumé" → "resume"
func normalizeUnicode(s string) string {
	// Remove accents (NFD decomposition, strip combining marks)
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, _ := transform.String(t, s)
	return result
}

// SynonymMap expands search queries with synonyms before FTS.
var synonymMap = map[string][]string{
	// Languages
	"golang":      {"golang", "Go"},
	"go":          {"Go", "golang"},
	"javascript":  {"javascript", "JS", "node"},
	"python":      {"python", "py"},
	"typescript":  {"typescript", "TS"},

	// Databases
	"postgres":    {"postgres", "PostgreSQL", "pg"},
	"postgresql":  {"postgresql", "postgres", "pg"},
	"database":    {"database", "db", "postgres", "PostgreSQL", "sql", "config"},
	"db":          {"db", "database", "postgres", "sql"},
	"clickhouse":  {"clickhouse", "ClickHouse", "analytics"},
	"redis":       {"redis", "Redis", "cache", "caching"},
	"mysql":       {"mysql", "MariaDB"},
	"sqlite":      {"sqlite", "SQLite"},
	"chromadb":    {"chromadb", "Chroma", "vector store"},
	"timescaledb": {"timescaledb", "Timescale"},

	// Frameworks
	"gin":         {"gin", "Go framework"},
	"chi":         {"chi", "Chi router", "HTTP router"},
	"echo":        {"echo", "Go framework"},
	"react":       {"react", "React", "frontend", "UI"},
	"vite":        {"vite", "Vite", "build", "dev server"},
	"express":     {"express", "expressjs", "Node framework"},

	// Auth
	"auth":        {"auth", "JWT", "token", "authentication", "login"},
	"jwt":         {"JWT", "auth", "token", "authentication"},
	"authenticate": {"authenticate", "auth", "JWT", "token", "login"},
	"authentication": {"authentication", "auth", "JWT", "token"},
	"authorization": {"authorization", "auth", "permission", "access"},
	"token":       {"token", "JWT", "auth", "access token"},
	"login":       {"login", "auth", "credentials", "password"},
	"credentials": {"credentials", "login", "password", "auth"},

	// Caching
	"cache":       {"cache", "caching", "Redis"},
	"caching":     {"caching", "cache", "Redis"},
	"caches":      {"caches", "caching", "cache", "Redis"},
	"cached":      {"cached", "caching", "cache", "Redis"},
	"memcached":   {"memcached", "cache"},

	// Logging
	"log":         {"log", "logging", "slog"},
	"logging":     {"logging", "log", "slog", "structured logging"},
	"slog":        {"slog", "structured logging", "log"},
	"printf":      {"printf", "log.Printf", "log"},
	"log.Printf":  {"log.Printf", "printf", "log", "logging"},

	// Search/Vector
	"vector":      {"vector", "pgvector", "embedding", "search"},
	"embedding":   {"embedding", "nomic-embed-text", "vector"},
	"embeddings":  {"embeddings", "embedding", "nomic-embed-text", "vector"},
	"pgvector":    {"pgvector", "vector", "embedding"},
	"openai":      {"openai", "ada", "embedding"},
	"semantic":    {"semantic", "vector", "embedding", "search"},

	// Deployment
	"deploy":      {"deploy", "deployment", "release", "ship", "release.sh"},
	"deployment":  {"deployment", "deploy", "release", "ship"},
	"release":     {"release", "deploy", "version", "release.sh"},
	"releases":    {"releases", "release", "deploy"},
	"server":      {"server", "VPS", "host", "IP"},
	"vps":         {"VPS", "server", "host", "IP"},
	"ip":          {"ip", "address", "server", "VPS"},
	"port":        {"port", "ports", "service", "endpoint"},
	"ports":       {"ports", "port", "service", "endpoint"},
	"nginx":       {"nginx", "nginx", "sites-enabled", "reverse proxy"},

	// Performance
	"performance": {"performance", "speed", "latency", "fast"},
	"slow":        {"slow", "performance", "O(N)", "bottleneck"},
	"latency":     {"latency", "performance", "speed", "ms"},
	"fast":        {"fast", "performance", "speed"},

	// Security
	"security":    {"security", "CORS", "whitelist", "auth"},
	"cors":        {"CORS", "security", "whitelist", "origin"},

	// Alternatives
	"alternative": {"alternative", "replacement", "option", "vs"},
	"replacement": {"replacement", "alternative", "successor"},
	"vs":          {"vs", "versus", "compared", "alternative"},
	"compared":    {"compared", "vs", "versus", "alternative"},

	// Problems
	"bug":         {"bug", "bugs", "issue", "problem", "broken"},
	"bugs":        {"bugs", "bug", "issues", "problems", "broken"},
	"broken":      {"broken", "bug", "issue", "corruption"},
	"corruption":  {"corruption", "broken", "bug", "issue"},
	"issue":       {"issue", "bug", "problem"},
	"issues":      {"issues", "bugs", "problems"},
	"error":       {"error", "bug", "issue", "problem"},
	"problem":     {"problem", "bug", "issue", "broken"},
	"problems":    {"problems", "bugs", "issues"},

	// Migration
	"migration":   {"migration", "SQL", "schema", "migrate"},
	"migrations":  {"migrations", "migration", "SQL"},
	"idempotent":  {"idempotent", "IF NOT EXISTS", "safe"},

	// Frontend
	"frontend":    {"frontend", "React", "UI", "interface"},
	"ui":          {"UI", "frontend", "interface"},
	"proxy":       {"proxy", "vite proxy", "reverse proxy"},

	// Infrastructure
	"docker":      {"docker", "Docker", "container", "compose"},
	"systemd":     {"systemd", "service", "daemon"},
	"cron":        {"cron", "cronjob", "scheduled", "scheduler"},
	"sessions":    {"sessions", "Redis", "session binding", "tmux", "session management"},
	"session":     {"session", "sessions", "tmux", "terminal"},
	"tmux":        {"tmux", "session", "terminal", "multiplexer"},
	"terminal":    {"terminal", "tmux", "CLI", "command line"},
	// Bot detection
	"bot":         {"bot", "bot detection", "crawler", "scraper"},
	"score":       {"score", "threshold", "bot score"},
	"threshold":   {"threshold", "score", "bot detection"},
	"ip list":     {"ip list", "IP lists", "bot IPs"},

	// Misc
	"compression": {"compression", "ClickHouse", "columnar"},
	"routing":     {"routing", "flow", "traffic routing"},
	"middleware":  {"middleware", "chi middleware", "interceptor"},
	"timeout":     {"timeout", "context timeout", "deadline"},
	"architecture": {"architecture", "design", "structure", "pgvector", "hybrid"},
	"algorithm":   {"algorithm", "search", "hybrid", "RRF", "ranking"},
	"search":      {"search", "FTS", "hybrid", "semantic", "vector"},

	// NEW: Transaction-related synonyms for better recall
	"transaction": {"transaction", "BEGIN", "COMMIT", "ROLLBACK", "ACID", "consistency"},
	"begin":       {"BEGIN", "transaction", "start"},
	"commit":      {"COMMIT", "transaction", "save", "persist"},
	"rollback":    {"ROLLBACK", "revert", "undo", "transaction"},
	"acid":        {"ACID", "atomicity", "consistency", "isolation", "durability"},
	"consistency": {"consistency", "ACID", "transaction", "integrity"},

	// NEW: Deployment/DevOps synonyms
	"bluegreen":   {"blue-green", "zero downtime", "deployment"},
	"canary":      {"canary", "gradual rollout", "deployment"},
	"revert":      {"revert", "undo", "rollback"},
	"undo":        {"undo", "revert", "rollback"},

	// NEW: Testing synonyms
	"test":        {"test", "testing", "validation", "verify", "assert"},
	"testing":     {"testing", "test", "TDD", "validation", "verify"},
	"tdd":         {"TDD", "test-driven development", "testing"},
	"verify":      {"verify", "validation", "check", "confirm"},
	"assert":      {"assert", "assertion", "verify", "check"},

	// NEW: API/Service synonyms
	"api":         {"API", "endpoint", "interface", "service", "REST"},
	"endpoint":    {"endpoint", "API", "route", "handler"},
	"rest":        {"REST", "API", "HTTP", "endpoint"},
	"graphql":     {"GraphQL", "API", "query", "mutation"},
	"grpc":        {"gRPC", "RPC", "protobuf", "service"},

	// NEW: Cloud/Infrastructure synonyms
	"kubernetes":  {"kubernetes", "k8s", "container orchestration"},
	"k8s":         {"k8s", "kubernetes", "container orchestration"},
	"aws":         {"AWS", "Amazon Web Services", "cloud"},
	"gcp":         {"GCP", "Google Cloud", "cloud"},
	"azure":       {"Azure", "Microsoft Cloud", "cloud"},
	"cloud":       {"cloud", "AWS", "GCP", "Azure", "infrastructure"},
	"terraform":   {"terraform", "IaC", "infrastructure as code"},
	"iac":         {"IaC", "infrastructure as code", "terraform"},

	// NEW: Monitoring/Observability synonyms
	"observability": {"observability", "monitoring", "tracing", "logging", "metrics"},
	"tracing":       {"tracing", "distributed tracing", "jaeger", "zipkin"},
	"metrics":       {"metrics", "prometheus", "grafana", "monitoring"},
	"prometheus":    {"prometheus", "metrics", "monitoring"},
	"grafana":       {"grafana", "dashboard", "metrics", "visualization"},
	"alert":         {"alert", "alerting", "notification", "pagerduty"},
	"pagerduty":     {"pagerduty", "incident", "alert", "oncall"},

	// NEW: CI/CD synonyms
	"cicd":        {"CI/CD", "continuous integration", "continuous deployment", "pipeline"},
	"pipeline":    {"pipeline", "CI/CD", "workflow", "automation"},
	"jenkins":     {"jenkins", "CI/CD", "automation"},
	"githubactions": {"GitHub Actions", "CI/CD", "workflow", "automation"},
	"gitlabci":    {"GitLab CI", "CI/CD", "pipeline", "automation"},
	"circleci":    {"CircleCI", "CI/CD", "pipeline", "automation"},
	"travis":      {"Travis CI", "CI/CD", "automation"},

	// NEW: Version Control synonyms
	"git":         {"git", "version control", "VCS", "repository"},
	"github":      {"GitHub", "git", "repository", "PR"},
	"gitlab":      {"GitLab", "git", "repository", "CI"},
	"bitbucket":   {"Bitbucket", "git", "repository"},
	"pr":          {"PR", "pull request", "merge request", "code review"},
	"merge":       {"merge", "PR", "integrate", "combine"},
	"branch":      {"branch", "git branch", "feature branch", "version"},
	"tag":         {"tag", "release tag", "version", "git tag"},
	"rebase":      {"rebase", "git rebase", "rewrite history"},
	"cherrypick":  {"cherry-pick", "git cherry-pick", "selective merge"},

	// NEW: Data synonyms
	"data":        {"data", "information", "dataset", "records"},
	"dataset":     {"dataset", "data", "collection", "records"},
	"etl":         {"ETL", "extract transform load", "data pipeline"},
	"datawarehouse": {"data warehouse", "DW", "analytics", "BI"},
	"datalake":    {"data lake", "raw data", "storage"},
	"kafka":       {"kafka", "streaming", "event bus", "messaging"},
	"rabbitmq":    {"RabbitMQ", "message queue", "messaging"},
	"sqs":         {"SQS", "message queue", "AWS", "messaging"},

	// NEW: ML/AI synonyms
	"ml":          {"ML", "machine learning", "AI", "model"},
	"machinelearning": {"machine learning", "ML", "AI", "deep learning"},
	"ai":          {"AI", "artificial intelligence", "ML", "model"},
	"model":       {"model", "ML model", "neural network", "algorithm"},
	"neuralnetwork": {"neural network", "deep learning", "NN", "model"},
	"deeplearning": {"deep learning", "neural network", "DL", "AI"},
	"training":    {"training", "model training", "fit", "learn"},
	"inference":   {"inference", "prediction", "model serving", "deploy"},
	"tensorflow":  {"tensorflow", "TF", "deep learning", "Google"},
	"pytorch":     {"pytorch", "torch", "deep learning", "Meta"},
	"scikitlearn": {"scikit-learn", "sklearn", "ML", "Python"},
	"xgboost":     {"xgboost", "gradient boosting", "ML", "ensemble"},
	"llm":         {"LLM", "large language model", "GPT", "transformer"},
	"gpt":         {"GPT", "OpenAI", "LLM", "transformer"},
	"transformer": {"transformer", "attention", "BERT", "GPT", "LLM"},
	"bert":        {"BERT", "transformer", "NLP", "Google"},
	"nlp":         {"NLP", "natural language processing", "text", "LLM"},
	"cv":          {"CV", "computer vision", "image", "CNN"},
	"cnn":         {"CNN", "convolutional neural network", "CV", "image"},
	"rnn":         {"RNN", "recurrent neural network", "LSTM", "sequence"},
	"lstm":        {"LSTM", "long short-term memory", "RNN", "sequence"},
	"gan":         {"GAN", "generative adversarial network", "generation"},
	"vae":         {"VAE", "variational autoencoder", "generation"},
	"reinforcement": {"reinforcement learning", "RL", "agent", "reward"},
	"rl":          {"RL", "reinforcement learning", "agent", "policy"},

	// NEW: Web synonyms
	"web":         {"web", "internet", "HTTP", "browser"},
	"http":        {"HTTP", "web", "protocol", "REST"},
	"https":       {"HTTPS", "TLS", "SSL", "secure"},
	"tls":         {"TLS", "SSL", "HTTPS", "encryption"},
	"ssl":         {"SSL", "TLS", "HTTPS", "encryption"},
	"websocket":   {"websocket", "ws", "real-time", "socket"},
	"cdn":         {"CDN", "content delivery network", "cache", "edge"},
	"dns":         {"DNS", "domain name system", "resolution"},
	"loadbalancer": {"load balancer", "LB", "HAProxy", "nginx", "traffic"},
	"haproxy":     {"HAProxy", "load balancer", "proxy"},
	"traefik":     {"traefik", "reverse proxy", "load balancer"},
	"envoy":       {"envoy", "proxy", "service mesh"},
	"istio":       {"istio", "service mesh", "kubernetes"},
	"linkerd":     {"linkerd", "service mesh", "kubernetes"},

	// NEW: Mobile synonyms
	"mobile":      {"mobile", "iOS", "Android", "app"},
	"swiftmobile": {"Swift", "iOS", "Apple", "mobile"},
	"kotlinmobile": {"Kotlin", "Android", "JetBrains", "mobile"},
	"flutter":     {"Flutter", "Dart", "cross-platform", "mobile"},
	"reactnative": {"React Native", "mobile", "cross-platform", "React"},
	"xamarin":     {"Xamarin", "C#", "mobile", "cross-platform"},

	// NEW: Language synonyms
	"java":        {"Java", "JVM", "Spring", "enterprise"},
	"csharp":      {"C#", ".NET", "Microsoft", "enterprise"},
	"dotnet":      {".NET", "C#", "Microsoft", "ASP.NET"},
	"aspnet":      {"ASP.NET", ".NET", "web", "Microsoft"},
	"spring":      {"Spring", "Java", "framework", "enterprise"},
	"springboot":  {"Spring Boot", "Spring", "Java", "microservices"},
	"rails":       {"Rails", "Ruby", "web", "framework"},
	"ruby":        {"Ruby", "Rails", "scripting"},
	"php":         {"PHP", "Laravel", "web", "scripting"},
	"laravel":     {"Laravel", "PHP", "web", "framework"},
	"nodejs":      {"Node.js", "JavaScript", "backend", "runtime"},
	"deno":        {"Deno", "JavaScript", "TypeScript", "runtime"},
	"bun":         {"Bun", "JavaScript", "runtime", "fast"},
	"rust":        {"Rust", "systems programming", "memory safe"},
	"cpp":         {"C++", "systems programming", "performance"},
	"c":           {"C", "systems programming", "embedded"},
	"haskell":     {"Haskell", "functional", "pure"},
	"scala":       {"Scala", "JVM", "functional", "Spark"},
	"elixir":      {"Elixir", "Erlang VM", "concurrent", "functional"},
	"erlang":      {"Erlang", "concurrent", "distributed", "fault-tolerant"},
	"clojure":     {"Clojure", "Lisp", "JVM", "functional"},
	"lua":         {"Lua", "scripting", "embedded", "game"},
	"perl":        {"Perl", "scripting", "text processing"},
	"r":           {"R", "statistics", "data science"},
	"matlab":      {"MATLAB", "math", "engineering"},
	"julia":       {"Julia", "scientific computing", "performance"},
	"groovy":      {"Groovy", "JVM", "scripting", "Gradle"},
	"dart":        {"Dart", "Flutter", "Google", "web"},
	"vba":         {"VBA", "Excel", "automation", "Microsoft"},
	"powershell":  {"PowerShell", "Windows", "automation", "scripting"},
	"bash":        {"bash", "shell", "scripting", "Linux"},
	"zsh":         {"zsh", "shell", "scripting", "macOS"},
	"fish":        {"fish", "shell", "user-friendly"},

	// NEW: Architecture synonyms
	"microservices": {"microservices", "distributed", "SOA", "services"},
	"soa":         {"SOA", "service oriented architecture", "microservices"},
	"monolith":    {"monolith", "monolithic", "single application"},
	"serverless":  {"serverless", "FaaS", "lambda", "cloud functions"},
	"faas":        {"FaaS", "function as a service", "serverless"},
	"lambda":      {"lambda", "AWS Lambda", "serverless"},
	"eventdriven": {"event-driven", "EDA", "messaging", "async"},
	"eda":         {"EDA", "event-driven architecture", "messaging"},
	"cqrs":        {"CQRS", "command query responsibility segregation"},
	"event sourcing": {"event sourcing", "CQRS", "audit", "history"},
	"saga":        {"saga", "distributed transaction", "compensation"},
	"circuitbreaker": {"circuit breaker", "resilience", "fault tolerance"},
	"bulkhead":    {"bulkhead", "resilience", "isolation"},
	"retry":       {"retry", "resilience", "exponential backoff"},
	"backoff":     {"backoff", "exponential backoff", "retry", "delay"},
	"resiliencetimeout": {"timeout", "circuit breaker", "resilience"},
	"ratelimiting": {"rate limiting", "throttling", "quota", "API"},
	"throttling":  {"throttling", "rate limiting", "quota"},
	"quota":       {"quota", "rate limiting", "throttling"},
	"loadshedding": {"load shedding", "degradation", "resilience"},
	"gracefuldegradation": {"graceful degradation", "resilience", "fallback"},
	"fallback":    {"fallback", "graceful degradation", "resilience"},

	// NEW: Storage synonyms
	"storage":     {"storage", "disk", "volume", "persistent"},
	"s3":          {"S3", "object storage", "AWS", "bucket"},
	"blob":        {"blob", "object storage", "Azure", "S3"},
	"nfs":         {"NFS", "network file system", "shared storage"},
	"san":         {"SAN", "storage area network", "enterprise"},
	"nas":         {"NAS", "network attached storage", "file"},
	"ssd":         {"SSD", "solid state drive", "fast storage"},
	"hdd":         {"HDD", "hard disk drive", "spinning disk"},
	"raid":        {"RAID", "redundancy", "disk array"},
	"backup":      {"backup", "snapshot", "restore", "disaster recovery"},
	"snapshot":    {"snapshot", "backup", "point in time"},
	"restore":     {"restore", "backup", "recovery", "rollback"},
	"replication": {"replication", "copy", "sync", "high availability"},
	"sharding":    {"sharding", "partition", "horizontal scaling"},
	"partition":   {"partition", "sharding", "split", "divide"},

	// NEW: Networking synonyms
	"network":     {"network", "networking", "TCP/IP", "connection"},
	"tcp":         {"TCP", "transmission control protocol", "reliable"},
	"udp":         {"UDP", "user datagram protocol", "fast"},
	"ipv4":        {"IPv4", "IP", "address"},
	"ipv6":        {"IPv6", "IP", "next generation"},
	"subnet":      {"subnet", "network segment", "CIDR"},
	"cidr":        {"CIDR", "subnet", "network"},
	"vlan":        {"VLAN", "virtual LAN", "network segmentation"},
	"vpn":         {"VPN", "virtual private network", "tunnel"},
	"firewall":    {"firewall", "security", "network", "iptables"},
	"iptables":    {"iptables", "firewall", "Linux", "netfilter"},
	"nftables":    {"nftables", "firewall", "Linux", "successor"},
	"nat":         {"NAT", "network address translation", "router"},
	"dhcp":        {"DHCP", "dynamic host configuration", "IP assignment"},
	"bgp":         {"BGP", "border gateway protocol", "routing"},
	"ospf":        {"OSPF", "open shortest path first", "routing"},
	"mpls":        {"MPLS", "multiprotocol label switching", "WAN"},
	"sdn":         {"SDN", "software defined networking", "controller"},
	"overlay":     {"overlay", "tunnel", "VXLAN", "network"},
	"vxlan":       {"VXLAN", "virtual extensible LAN", "overlay"},
	"gre":         {"GRE", "generic routing encapsulation", "tunnel"},
	"ipsec":       {"IPsec", "VPN", "encryption", "security"},
	"wireguard":   {"WireGuard", "VPN", "modern", "fast"},
	"openvpn":     {"OpenVPN", "VPN", "SSL/TLS", "tunnel"},
	"linux":       {"Linux", "kernel", "open source", "OS"},
	"ubuntu":      {"Ubuntu", "Linux", "Debian", "Canonical"},
	"debian":      {"Debian", "Linux", "stable", "distro"},
	"centos":      {"CentOS", "Linux", "RHEL", "Red Hat"},
	"rhel":        {"RHEL", "Red Hat Enterprise Linux", "enterprise"},
	"fedora":      {"Fedora", "Linux", "Red Hat", "bleeding edge"},
	"alpine":      {"Alpine", "Linux", "Docker", "minimal"},
	"arch":        {"Arch", "Linux", "rolling release", "pacman"},
	"gentoo":      {"Gentoo", "Linux", "source based", "portage"},
	"opensuse":    {"openSUSE", "Linux", "SUSE", "YaST"},
	"suse":        {"SUSE", "Linux", "enterprise", "German"},
	"nixos":       {"NixOS", "Linux", "declarative", "reproducible"},
	"freebsd":     {"FreeBSD", "BSD", "Unix", "server"},
	"openbsd":     {"OpenBSD", "BSD", "secure", "Unix"},
	"netbsd":      {"NetBSD", "BSD", "portable", "Unix"},
	"dragonfly":   {"DragonFly BSD", "BSD", "HAMMER", "Unix"},
	"solaris":     {"Solaris", "Unix", "Oracle", "enterprise"},
	"illumos":     {"illumos", "OpenSolaris", "Unix", "open source"},
	"windows":     {"Windows", "Microsoft", "OS", "desktop"},
	"windowsserver": {"Windows Server", "Microsoft", "enterprise", "OS"},
	"macos":       {"macOS", "Apple", "Darwin", "desktop"},
	"ios":         {"iOS", "Apple", "mobile", "iPhone"},
	"android":     {"Android", "Google", "mobile", "Linux"},

	// NEW: Virtualization synonyms
	"vm":          {"VM", "virtual machine", "hypervisor", "guest"},
	"virtualmachine": {"virtual machine", "VM", "hypervisor"},
	"hypervisor":  {"hypervisor", "VMM", "virtual machine monitor"},
	"kvm":         {"KVM", "kernel virtual machine", "Linux", "hypervisor"},
	"xen":         {"Xen", "hypervisor", "paravirtualization"},
	"vmware":      {"VMware", "vSphere", "hypervisor", "enterprise"},
	"virtualbox":  {"VirtualBox", "Oracle", "hypervisor", "desktop"},
	"parallels":   {"Parallels", "macOS", "hypervisor", "desktop"},
	"qemu":        {"QEMU", "emulator", "hypervisor", "KVM"},
	"proxmox":     {"Proxmox", "KVM", "container", "hypervisor"},
	"lxc":         {"LXC", "Linux container", "container"},
	"lxd":         {"LXD", "LXC", "container", "hypervisor"},
	"containerd":  {"containerd", "container runtime", "Docker", "CNCF"},
	"crio":        {"CRI-O", "container runtime", "Kubernetes", "OCI"},
	"runc":        {"runc", "container runtime", "OCI", "Docker"},
	"podman":      {"Podman", "container", "Docker alternative", "rootless"},
	"buildah":     {"Buildah", "container image", "OCI", "Red Hat"},
	"skopeo":      {"Skopeo", "container image", "copy", "inspect"},

	// NEW: Editor/IDE synonyms
	"vscode":      {"VS Code", "Visual Studio Code", "editor", "Microsoft"},
	"vim":         {"Vim", "vi", "editor", "modal"},
	"neovim":      {"Neovim", "nvim", "Vim", "modern"},
	"emacs":       {"Emacs", "editor", "Lisp", "GNU"},
	"sublime":     {"Sublime Text", "editor", "fast"},
	"atom":        {"Atom", "editor", "GitHub", "Electron"},
	"intellij":    {"IntelliJ", "JetBrains", "IDE", "Java"},
	"pycharm":     {"PyCharm", "JetBrains", "IDE", "Python"},
	"goland":      {"GoLand", "JetBrains", "IDE", "Go"},
	"webstorm":    {"WebStorm", "JetBrains", "IDE", "JavaScript"},
	"phpstorm":    {"PhpStorm", "JetBrains", "IDE", "PHP"},
	"rider":       {"Rider", "JetBrains", "IDE", ".NET"},
	"clion":       {"CLion", "JetBrains", "IDE", "C++"},
	"rubymine":    {"RubyMine", "JetBrains", "IDE", "Ruby"},
	"eclipse":     {"Eclipse", "IDE", "Java", "IBM"},
	"netbeans":    {"NetBeans", "IDE", "Java", "Apache"},
	"xcode":       {"Xcode", "Apple", "IDE", "macOS"},
	"androidstudio": {"Android Studio", "Google", "IDE", "Android"},

	// NEW: Project Management synonyms
	"agile":       {"agile", "scrum", "kanban", "iterative"},
	"scrum":       {"scrum", "agile", "sprint", "ceremony"},
	"kanban":      {"kanban", "agile", "visual", "flow"},
	"sprint":      {"sprint", "scrum", "iteration", "cycle"},
	"backlog":     {"backlog", "user stories", "product", "priorities"},
	"story":       {"user story", "story", "requirement", "feature"},
	"epic":        {"epic", "large story", "theme", "initiative"},
	"jira":        {"Jira", "Atlassian", "issue tracker", "agile"},
	"trello":      {"Trello", "Atlassian", "kanban", "visual"},
	"asana":       {"Asana", "project management", "task", "team"},
	"monday":      {"Monday.com", "project management", "work OS"},
	"notion":      {"Notion", "wiki", "docs", "project management"},
	"confluence":  {"Confluence", "Atlassian", "wiki", "documentation"},
	"slack":       {"Slack", "messaging", "team", "communication"},
	"teams":       {"Teams", "Microsoft Teams", "messaging", "video"},
	"discord":     {"Discord", "messaging", "community", "voice"},
	"zoom":        {"Zoom", "video conferencing", "meeting", "webinar"},
	"meet":        {"Google Meet", "video conferencing", "meeting"},
	"webex":       {"Webex", "Cisco", "video conferencing", "meeting"},

	// NEW: Documentation synonyms
	"documentation": {"documentation", "docs", "wiki", "README"},
	"readme":      {"README", "documentation", "getting started"},
	"changelog":   {"changelog", "release notes", "history", "changes"},
	"api docs":    {"API docs", "swagger", "openapi", "reference"},
	"swagger":     {"swagger", "OpenAPI", "API docs", "specification"},
	"openapi":     {"OpenAPI", "swagger", "API specification", "REST"},
	"postman":     {"Postman", "API testing", "collection", "HTTP"},
	"insomnia":    {"Insomnia", "API testing", "HTTP client", "REST"},
	"graphql playground": {"GraphQL Playground", "GraphQL", "explorer", "API"},
	"storybook":   {"Storybook", "UI components", "documentation", "React"},

	// NEW: Testing types synonyms
	"unittest":    {"unit test", "unit testing", "TDD", "isolated"},
	"integrationtest": {"integration test", "integration testing", "end-to-end"},
	"e2e":         {"end-to-end", "E2E", "integration", "full flow"},
	"regressiontest": {"regression test", "regression", "prevent bugs"},
	"smoketest":   {"smoke test", "smoke", "basic verification"},
	"sanitytest":  {"sanity test", "sanity", "quick check"},
	"performancetest": {"performance test", "load test", "stress test", "benchmark"},
	"loadtest":    {"load test", "performance", "concurrency", "capacity"},
	"stresstest":  {"stress test", "performance", "breaking point", "limits"},
	"benchmarktest": {"benchmark", "performance", "measurement", "comparison"},
	"chaostest":   {"chaos engineering", "chaos", "resilience", "failure injection"},
	"fuzztest":    {"fuzz testing", "fuzz", "random input", "security"},
	"mutationtest": {"mutation testing", "mutation", "code quality", "coverage"},
	"propertytest": {"property-based testing", "property", "generative", "QuickCheck"},
	"contracttest": {"contract testing", "Pact", "consumer-driven", "API"},
	"visualtest":  {"visual testing", "screenshot", "UI", "regression"},
	"accessibilitytest": {"accessibility testing", "a11y", "WCAG", "screen reader"},
	"securitytest": {"security testing", "penetration test", "vulnerability", "audit"},
	"penetrationtest": {"penetration test", "pentest", "security", "ethical hacking"},
	"vulnerabilitytest": {"vulnerability scan", "security", "CVE", "exploit"},

	// NEW: Code Quality synonyms
	"lint":        {"lint", "linter", "static analysis", "code style"},
	"linter":      {"linter", "lint", "static analysis", "code quality"},
	"formatter":   {"formatter", "format", "prettier", "gofmt"},
	"prettier":    {"prettier", "formatter", "JavaScript", "code style"},
	"gofmt":       {"gofmt", "go fmt", "formatter", "Go"},
	"black":       {"black", "formatter", "Python", "code style"},
	"eslint":      {"ESLint", "linter", "JavaScript", "code quality"},
	"pylint":      {"pylint", "linter", "Python", "code quality"},
	"golint":      {"golint", "linter", "Go", "code style"},
	"staticanalysis": {"static analysis", "SAST", "code quality", "security"},
	"sast":        {"SAST", "static application security testing", "security"},
	"dast":        {"DAST", "dynamic application security testing", "security"},
	"iast":        {"IAST", "interactive application security testing", "security"},
	"sca":         {"SCA", "software composition analysis", "dependencies", "security"},
	"sbom":        {"SBOM", "software bill of materials", "dependencies", "supply chain"},
	"dependency":  {"dependency", "library", "package", "module"},
	"vulnerability": {"vulnerability", "CVE", "security", "exploit"},
	"cve":         {"CVE", "common vulnerabilities and exposures", "security"},
	"cwe":         {"CWE", "common weakness enumeration", "security"},
	"owasp":       {"OWASP", "web security", "top 10", "guidelines"},
	"nist":        {"NIST", "standards", "cybersecurity", "framework"},
	"iso27001":    {"ISO 27001", "information security", "standard", "audit"},
	"soc2":        {"SOC 2", "security", "compliance", "audit"},
	"gdpr":        {"GDPR", "privacy", "EU", "data protection"},
	"ccpa":        {"CCPA", "privacy", "California", "data protection"},
	"hipaa":       {"HIPAA", "healthcare", "privacy", "compliance"},
	"pci":         {"PCI DSS", "payment card", "security", "compliance"},
	"fedramp":     {"FedRAMP", "government", "cloud", "security"},
}

// ExpandQuery expands a search query with synonyms.
// Returns an expanded query string for FTS, and a list of terms for LIKE fallback.
func ExpandQuery(query string) (expanded string, terms []string) {
	// Normalize unicode first (café → cafe)
	query = normalizeUnicode(query)
	lower := strings.ToLower(query)
	words := strings.Fields(lower)
	seen := make(map[string]bool)

	// Start with original query
	expanded = query
	terms = append(terms, query)

	for _, word := range words {
		// Strip punctuation for matching
		clean := strings.Trim(word, ".,!?;:()[]{}\"'")
		if syns, ok := synonymMap[clean]; ok {
			for _, syn := range syns {
				if !seen[strings.ToLower(syn)] {
					seen[strings.ToLower(syn)] = true
					terms = append(terms, syn)
				}
			}
		}
	}

	// Build expanded query for FTS (OR all synonyms)
	if len(terms) > 1 {
		expanded = strings.Join(terms, " OR ")
	}

	return expanded, terms
}

// EmbeddingCache caches query embeddings to avoid repeated Ollama calls.
type EmbeddingCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	maxAge  time.Duration
	maxSize int
}

type cacheEntry struct {
	embedding []float32
	createdAt time.Time
}

// NewEmbeddingCache creates a new embedding cache.
func NewEmbeddingCache() *EmbeddingCache {
	return &EmbeddingCache{
		entries: make(map[string]cacheEntry),
		maxAge:  30 * time.Minute,
		maxSize: 5000,
	}
}

// Get retrieves a cached embedding.
func (c *EmbeddingCache) Get(key string) ([]float32, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Since(entry.createdAt) > c.maxAge {
		return nil, false
	}
	return entry.embedding, true
}

// Set stores an embedding in the cache.
func (c *EmbeddingCache) Set(key string, embedding []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity
	if len(c.entries) >= c.maxSize {
		var oldest string
		var oldestTime time.Time
		for k, v := range c.entries {
			if oldestTime.IsZero() || v.createdAt.Before(oldestTime) {
				oldest = k
				oldestTime = v.createdAt
			}
		}
		delete(c.entries, oldest)
	}

	c.entries[key] = cacheEntry{
		embedding: embedding,
		createdAt: time.Now(),
	}
}

// Stats returns cache statistics.
func (c *EmbeddingCache) Stats() (size int, maxAge time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries), c.maxAge
}
