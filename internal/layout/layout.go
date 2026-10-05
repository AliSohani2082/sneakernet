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

	CommandLink = "/usr/local/bin/sneakernet"

	// ServiceUser runs Xray. It is created with systemd-sysusers from SysusersFile.
	ServiceUser  = "sneakernet"
	SysusersFile = "/etc/sysusers.d/sneakernet.conf"

	UnitName = "sneakernet-xray.service"
	UnitDir  = "/etc/systemd/system"
	UnitFile = UnitDir + "/" + UnitName
)

// GeoFiles are the routing data files shipped with the Xray core.
var GeoFiles = []string{"geoip.dat", "geosite.dat"}
