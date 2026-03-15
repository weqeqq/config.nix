package installer

type Phase string

const (
	PhasePrepare   Phase = "prepare"
	PhaseDetect    Phase = "detect"
	PhasePartition Phase = "partition"
	PhaseHardware  Phase = "hardware"
	PhaseSecrets   Phase = "secrets"
	PhasePersist   Phase = "persist"
	PhaseInstall   Phase = "install"
)

var PhaseOrder = []Phase{
	PhasePrepare,
	PhaseDetect,
	PhasePartition,
	PhaseHardware,
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

type SharedSettings struct {
	System             string `json:"system"`
	HostNamePrefix     string `json:"hostNamePrefix"`
	TimeZone           string `json:"timeZone"`
	Locale             string `json:"locale"`
	ConsoleKeyMap      string `json:"consoleKeyMap"`
	SystemStateVersion string `json:"systemStateVersion"`
	HomeStateVersion   string `json:"homeStateVersion"`
	Graphics           struct {
		NVIDIA struct {
			Open bool `json:"open"`
		} `json:"nvidia"`
	} `json:"graphics"`
	Boot struct {
		SecureBoot struct {
			Enable    bool   `json:"enable"`
			PkiBundle string `json:"pkiBundle"`
		} `json:"secureBoot"`
	} `json:"boot"`
	User struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		ExtraGroups []string `json:"extraGroups"`
		OpenSSH     struct {
			AuthorizedKeys []string `json:"authorizedKeys"`
		} `json:"openssh"`
	} `json:"user"`
	OwnerAgeRecipients []string `json:"ownerAgeRecipients"`
}

type InstallPlan struct {
	DeferredFeatures []string `json:"deferredFeatures"`
	FinalOutput      string   `json:"finalOutput"`
	InitialOutput    string   `json:"initialOutput"`
	InstallOutput    string   `json:"installOutput"`
	NeedsFinalize    bool     `json:"needsFinalize"`
}

type PlatformState struct {
	Kind       string `json:"kind"`
	Hypervisor string `json:"hypervisor"`
}

type GraphicsState struct {
	Vendor      string   `json:"vendor"`
	Enable32Bit bool     `json:"enable32Bit"`
	PCIIDs      []string `json:"pciIds"`
}

type MachineState struct {
	MachineID   string        `json:"machineId"`
	HostName    string        `json:"hostName"`
	InstalledAt string        `json:"installedAt"`
	InstallDisk string        `json:"installDisk"`
	Platform    PlatformState `json:"platform"`
	Graphics    GraphicsState `json:"graphics"`
}

type RuntimeSecrets struct {
	UserPasswordHash string `json:"userPasswordHash"`
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
	Mode                SecretMode
	Encrypted           bool
	Decryptable         bool
	HasSecret           bool
	SecretPath          string
	ActiveAgeKeyFile    string
	SuggestedAgeKeyFile string
}

type Session struct {
	Preflight   Preflight
	RepoRoot    string
	Disks       []DiskRecord
	UserName    string
	InstallPlan InstallPlan
	Detected    MachineState
}

type InstallRequest struct {
	RepoRoot     string
	Disk         string
	MountPoint   string
	AgeKeyFile   string
	SecretMode   SecretMode
	Password     string
	LUKSPassword string
}
