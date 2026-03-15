package installer

type Phase string

const (
	PhasePrepare   Phase = "prepare"
	PhasePartition Phase = "partition"
	PhaseHardware  Phase = "hardware"
	PhaseHostKey   Phase = "host-key"
	PhaseSecrets   Phase = "secrets"
	PhasePersist   Phase = "persist"
	PhaseInstall   Phase = "install"
)

var PhaseOrder = []Phase{
	PhasePrepare,
	PhasePartition,
	PhaseHardware,
	PhaseHostKey,
	PhaseSecrets,
	PhasePersist,
	PhaseInstall,
}

type EventKind string

const (
	EventPhaseStart    EventKind = "phase-start"
	EventPhaseLog      EventKind = "phase-log"
	EventPhaseComplete EventKind = "phase-complete"
	EventPhaseFailed   EventKind = "phase-failed"
	EventInstallDone   EventKind = "install-complete"
)

type SecretMode string

const (
	SecretModeCreate      SecretMode = "create"
	SecretModeReuse       SecretMode = "reuse"
	SecretModeNeedsAgeKey SecretMode = "needs-age-key"
	SecretModeReplace     SecretMode = "replace"
)

type Event struct {
	Kind          EventKind
	Phase         Phase
	Message       string
	RawLine       string
	InstallResult *InstallResult
}

type InstallResult struct {
	Host          string
	Disk          string
	InitialOutput string
	FinalOutput   string
	NeedsFinalize bool
	RepoPath      string
	ReceiptPath   string
}

type Preflight struct {
	UEFI          bool
	Revision      string
	RepoRoot      string
	SourceKind    string
	RequiredTools map[string]bool
}

type HostRecord struct {
	Host             string
	User             string
	HostName         string
	InitialOutput    string
	FinalOutput      string
	NeedsFinalize    bool
	DeferredFeatures []string
	OwnerRecipients  []string
}

type DiskRecord struct {
	Path          string
	PreferredPath string
	Size          string
	Model         string
	Transport     string
	Serial        string
	Mountpoints   []string
	IsLiveMedia   bool
}

type SecretStatus struct {
	Host                string
	Mode                SecretMode
	Encrypted           bool
	Decryptable         bool
	HasSecret           bool
	HostSecretPath      string
	ActiveAgeKeyFile    string
	SuggestedAgeKeyFile string
}

type Session struct {
	Preflight Preflight
	RepoRoot  string
	Hosts     []HostRecord
	Disks     []DiskRecord
}

type InstallRequest struct {
	RepoRoot     string
	Host         string
	Disk         string
	MountPoint   string
	AgeKeyFile   string
	SecretMode   SecretMode
	Password     string
	LUKSPassword string
}
