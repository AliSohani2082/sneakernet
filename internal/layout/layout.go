// Package layout names every path sneakernet installs, as seen from inside
// the target system. Our own directories avoid clashing with an existing
// Xray install (which typically uses /usr/local/bin/xray and xray.service).
package layout

const (
	OptDir   = "/opt/sneakernet"
	BinDir   = OptDir + "/bin"
	XrayBin  = BinDir + "/xray"
	SelfBin  = BinDir + "/sneakernet"
	AssetDir = OptDir + "/share" // geoip.dat, geosite.dat (XRAY_LOCATION_ASSET)

	EtcDir      = "/etc/sneakernet"
	ConfigFile  = EtcDir + "/config.json"
	ServersFile = EtcDir + "/servers.txt"
	StateFile   = EtcDir + "/state.json"

	VarDir       = "/var/lib/sneakernet"
	ManifestFile = VarDir + "/manifest.json"
	LogFile      = "/var/log/sneakernet-install.log"

	// CommandLink is where `sneakernet` is linked when /usr/local/bin is on
	// PATH; otherwise another PATH directory is used (see install).
	CommandLink = "/usr/local/bin/sneakernet"
	CommandName = "sneakernet"

	// ServiceUser runs Xray. It is created with systemd-sysusers from SysusersFile.
	ServiceUser  = "sneakernet"
	SysusersFile = "/etc/sysusers.d/sneakernet.conf"
	// RuntimeSysusersFile is used when /etc/sysusers.d is read-only (NixOS).
	RuntimeSysusersFile = "/run/sysusers.d/sneakernet.conf"

	UnitName = "sneakernet-xray.service"
	UnitDir  = "/etc/systemd/system"
	UnitFile = UnitDir + "/" + UnitName
	// RuntimeUnitDir holds the unit when UnitDir is read-only (NixOS keeps
	// it in the Nix store). Units there last until the next reboot.
	RuntimeUnitDir = "/run/systemd/system"
)

// GeoFiles are the routing data files shipped with the Xray core.
var GeoFiles = []string{"geoip.dat", "geosite.dat"}
