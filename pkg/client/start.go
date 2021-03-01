package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Go-Lift-TV/discordnotifier-client/pkg/apps"
	"github.com/Go-Lift-TV/discordnotifier-client/pkg/logs"
	"github.com/Go-Lift-TV/discordnotifier-client/pkg/plex"
	"github.com/Go-Lift-TV/discordnotifier-client/pkg/snapshot"
	"github.com/Go-Lift-TV/discordnotifier-client/pkg/ui"
	flag "github.com/spf13/pflag"
	"golift.io/cnfg"
	"golift.io/version"
)

// Application Defaults.
const (
	Title            = "DiscordNotifier Client"
	DefaultName      = "discordnotifier-client"
	DefaultLogFileMb = 100
	DefaultLogFiles  = 0 // delete none
	DefaultTimeout   = time.Minute
	DefaultBindAddr  = "0.0.0.0:5454"
	DefaultEnvPrefix = "DN"
)

// Flags are our CLI input flags.
type Flags struct {
	*flag.FlagSet
	verReq     bool
	ConfigFile string
	EnvPrefix  string
	TestSnaps  bool
}

// Config represents the data in our config file.
type Config struct {
	BindAddr   string           `json:"bind_addr" toml:"bind_addr" xml:"bind_addr" yaml:"bind_addr"`
	SSLCrtFile string           `json:"ssl_cert_file" toml:"ssl_cert_file" xml:"ssl_cert_file" yaml:"ssl_cert_file"`
	SSLKeyFile string           `json:"ssl_key_file" toml:"ssl_key_file" xml:"ssl_key_file" yaml:"ssl_key_file"`
	Upstreams  []string         `json:"upstreams" toml:"upstreams" xml:"upstreams" yaml:"upstreams"`
	Timeout    cnfg.Duration    `json:"timeout" toml:"timeout" xml:"timeout" yaml:"timeout"`
	Plex       *plex.Server     `json:"plex" toml:"plex" xml:"plex" yaml:"plex"`
	Snapshot   *snapshot.Config `json:"snapshot" toml:"snapshot" xml:"snapshot" yaml:"snapshot"`
	*logs.Logs
	*apps.Apps
}

// Client stores all the running data.
type Client struct {
	*logs.Logger
	Flags  *Flags
	Config *Config
	server *http.Server
	sigkil chan os.Signal
	sighup chan os.Signal
	allow  allowedIPs
	menu   map[string]ui.MenuItem
	info   string
	alert  *logs.Cooler
	plex   *logs.Cooler
}

// Errors returned by this package.
var (
	ErrNilAPIKey = fmt.Errorf("API key may not be empty: set a key in config file or with environment variable")
	ErrNoApps    = fmt.Errorf("at least 1 Starr app must be setup in config file or with environment variables")
)

// NewDefaults returns a new Client pointer with default settings.
func NewDefaults() *Client {
	return &Client{
		sigkil: make(chan os.Signal, 1),
		sighup: make(chan os.Signal, 1),
		menu:   make(map[string]ui.MenuItem),
		plex:   &logs.Cooler{},
		alert:  &logs.Cooler{},
		Logger: logs.New(),
		Config: &Config{
			Apps: &apps.Apps{
				URLBase: "/",
			},
			BindAddr: DefaultBindAddr,
			Logs: &logs.Logs{
				LogFiles:  DefaultLogFiles,
				LogFileMb: DefaultLogFileMb,
			},
			Timeout:  cnfg.Duration{Duration: DefaultTimeout},
			Snapshot: &snapshot.Config{},
		}, Flags: &Flags{
			FlagSet:    flag.NewFlagSet(DefaultName, flag.ExitOnError),
			ConfigFile: os.Getenv(DefaultEnvPrefix + "_CONFIG_FILE"),
			EnvPrefix:  DefaultEnvPrefix,
		},
	}
}

// ParseArgs stores the cli flag data into the Flags pointer.
func (f *Flags) ParseArgs(args []string) {
	f.StringVarP(&f.ConfigFile, "config", "c", os.Getenv(DefaultEnvPrefix+"_CONFIG_FILE"), f.Name()+" Config File")
	f.BoolVar(&f.TestSnaps, "snaps", false, f.Name()+"Test Snapshots")
	f.StringVarP(&f.EnvPrefix, "prefix", "p", DefaultEnvPrefix, "Environment Variable Prefix")
	f.BoolVarP(&f.verReq, "version", "v", false, "Print the version and exit.")
	f.Parse(args) // nolint: errcheck
}

// Start runs the app.
func Start() error {
	err := start()
	if err != nil {
		_, _ = ui.Error(Title, err.Error())
	}

	return err
}

func start() error {
	c := NewDefaults()
	c.Flags.ParseArgs(os.Args[1:])

	if c.Flags.verReq {
		fmt.Println(version.Print(c.Flags.Name()))
		return nil // print version and exit.
	}

	msg, err := c.getConfig()
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}

	if ui.HasGUI() {
		// Setting AppName forces log files (even if not configured).
		// Used for GUI apps that have no console output.
		c.Config.Logs.AppName = c.Flags.Name()
	}

	c.Logger.SetupLogging(c.Config.Logs)
	c.Printf("%s v%s-%s Starting! [PID: %v]", c.Flags.Name(), version.Version, version.Revision, os.Getpid())
	c.Printf("==> %s", msg)
	c.startPlex()
	c.startSnaps()

	if c.Flags.TestSnaps {
		c.testSnaps(snapshot.NotifiarrTestURL)
		return nil
	}

	return c.run(strings.HasPrefix(msg, msgConfigCreate))
}

// starts plex if it's configured. logs any error.
func (c *Client) startPlex() bool {
	var err error

	if c.Config.Plex != nil {
		c.Config.Plex.Logger = c.Logger

		if err = c.Config.Plex.Start(); err != nil {
			c.Errorf("plex config: %v (plex DISABLED)", err)
			c.Config.Plex = nil
		}
	}

	return err != nil
}

// starts snapshots if it's configured. logs any error.
func (c *Client) startSnaps() {
	if c.Config.Snapshot != nil {
		c.Config.Snapshot.Logger = c.Logger
		c.Config.Snapshot.Start()
	}
}

func (c *Client) run(newConfig bool) error {
	if c.Config.APIKey == "" {
		return fmt.Errorf("%w %s_API_KEY", ErrNilAPIKey, c.Flags.EnvPrefix)
	} else if len(c.Config.Radarr) < 1 && len(c.Config.Readarr) < 1 &&
		len(c.Config.Sonarr) < 1 && len(c.Config.Lidarr) < 1 {
		return ErrNoApps
	}

	c.PrintStartupInfo()
	signal.Notify(c.sigkil, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	signal.Notify(c.sighup, syscall.SIGHUP)

	if newConfig {
		_ = ui.OpenFile(c.Flags.ConfigFile)
		_, _ = ui.Warning(Title, "A new configuration file was created @ "+
			c.Flags.ConfigFile+" - it should open in a text editor. "+
			"Please edit the file and reload this application using the tray menu.")
	}

	switch ui.HasGUI() {
	case true:
		c.startTray() // This starts the web server.
		return nil    // startTray() calls os.Exit()
	default:
		c.StartWebServer()
		return c.Exit()
	}
}

// Temporary code?
func (c *Client) testSnaps(send string) {
	snaps, errs := c.Config.Snapshot.GetSnapshot()
	if len(errs) > 0 {
		for _, err := range errs {
			if err != nil {
				c.Errorf("%v", err)
			}
		}
	}

	p, err := c.Config.Plex.GetSessions()
	if err != nil {
		c.Errorf("%v", err)
	}

	b, _ := json.Marshal(&struct {
		*snapshot.Snapshot
		Plex *plex.Sessions `json:"plex"`
	}{Snapshot: snaps, Plex: p})

	c.Printf("Snapshot Data:\n%s", string(b))

	if send == "" {
		return
	}

	if body, err := snapshot.SendJSON(send, b); err != nil {
		c.Errorf("POSTING: %v: %s", err, string(body))
	} else {
		c.Printf("Sent Test Snapshot to %s", send)
	}
}
