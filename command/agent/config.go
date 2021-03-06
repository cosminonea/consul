package agent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/consul/consul"
	"github.com/hashicorp/consul/watch"
	"github.com/mitchellh/mapstructure"
)

// Ports is used to simplify the configuration by
// providing default ports, and allowing the addresses
// to only be specified once
type PortConfig struct {
	DNS     int // DNS Query interface
	HTTP    int // HTTP API
	HTTPS   int // HTTPS API
	RPC     int // CLI RPC
	SerfLan int `mapstructure:"serf_lan"` // LAN gossip (Client + Server)
	SerfWan int `mapstructure:"serf_wan"` // WAN gossip (Server onlyg)
	Server  int // Server internal RPC
}

// AddressConfig is used to provide address overrides
// for specific services. By default, either ClientAddress
// or ServerAddress is used.
type AddressConfig struct {
	DNS   string // DNS Query interface
	HTTP  string // HTTP API
	HTTPS string // HTTPS API
	RPC   string // CLI RPC
}

// DNSConfig is used to fine tune the DNS sub-system.
// It can be used to control cache values, and stale
// reads
type DNSConfig struct {
	// NodeTTL provides the TTL value for a node query
	NodeTTL    time.Duration `mapstructure:"-"`
	NodeTTLRaw string        `mapstructure:"node_ttl" json:"-"`

	// ServiceTTL provides the TTL value for a service
	// query for given service. The "*" wildcard can be used
	// to set a default for all services.
	ServiceTTL    map[string]time.Duration `mapstructure:"-"`
	ServiceTTLRaw map[string]string        `mapstructure:"service_ttl" json:"-"`

	// AllowStale is used to enable lookups with stale
	// data. This gives horizontal read scalability since
	// any Consul server can service the query instead of
	// only the leader.
	AllowStale bool `mapstructure:"allow_stale"`

	// EnableTruncate is used to enable setting the truncate
	// flag for UDP DNS queries.  This allows unmodified
	// clients to re-query the consul server using TCP
	// when the total number of records exceeds the number
	// returned by default for UDP.
	EnableTruncate bool `mapstructure:"enable_truncate"`

	// MaxStale is used to bound how stale of a result is
	// accepted for a DNS lookup. This can be used with
	// AllowStale to limit how old of a value is served up.
	// If the stale result exceeds this, another non-stale
	// stale read is performed.
	MaxStale    time.Duration `mapstructure:"-"`
	MaxStaleRaw string        `mapstructure:"max_stale" json:"-"`

	// OnlyPassing is used to determine whether to filter nodes
	// whose health checks are in any non-passing state. By
	// default, only nodes in a critical state are excluded.
	OnlyPassing bool `mapstructure:"only_passing"`
}

// Config is the configuration that can be set for an Agent.
// Some of this is configurable as CLI flags, but most must
// be set using a configuration file.
type Config struct {
	// Bootstrap is used to bring up the first Consul server, and
	// permits that node to elect itself leader
	Bootstrap bool `mapstructure:"bootstrap"`

	// BootstrapExpect tries to automatically bootstrap the Consul cluster,
	// by witholding peers until enough servers join.
	BootstrapExpect int `mapstructure:"bootstrap_expect"`

	// Server controls if this agent acts like a Consul server,
	// or merely as a client. Servers have more state, take part
	// in leader election, etc.
	Server bool `mapstructure:"server"`

	// Datacenter is the datacenter this node is in. Defaults to dc1
	Datacenter string `mapstructure:"datacenter"`

	// DataDir is the directory to store our state in
	DataDir string `mapstructure:"data_dir"`

	// DNSRecursors can be set to allow the DNS servers to recursively
	// resolve non-consul domains. It is deprecated, and merges into the
	// recursors array.
	DNSRecursor string `mapstructure:"recursor"`

	// DNSRecursors can be set to allow the DNS servers to recursively
	// resolve non-consul domains
	DNSRecursors []string `mapstructure:"recursors"`

	// DNS configuration
	DNSConfig DNSConfig `mapstructure:"dns_config"`

	// Domain is the DNS domain for the records. Defaults to "consul."
	Domain string `mapstructure:"domain"`

	// Encryption key to use for the Serf communication
	EncryptKey string `mapstructure:"encrypt" json:"-"`

	// LogLevel is the level of the logs to putout
	LogLevel string `mapstructure:"log_level"`

	// Node name is the name we use to advertise. Defaults to hostname.
	NodeName string `mapstructure:"node_name"`

	// ClientAddr is used to control the address we bind to for
	// client services (DNS, HTTP, HTTPS, RPC)
	ClientAddr string `mapstructure:"client_addr"`

	// BindAddr is used to control the address we bind to.
	// If not specified, the first private IP we find is used.
	// This controls the address we use for cluster facing
	// services (Gossip, Server RPC)
	BindAddr string `mapstructure:"bind_addr"`

	// AdvertiseAddr is the address we use for advertising our Serf,
	// and Consul RPC IP. If not specified, bind address is used.
	AdvertiseAddr string `mapstructure:"advertise_addr"`

	// Port configurations
	Ports PortConfig

	// Address configurations
	Addresses AddressConfig

	// LeaveOnTerm controls if Serf does a graceful leave when receiving
	// the TERM signal. Defaults false. This can be changed on reload.
	LeaveOnTerm bool `mapstructure:"leave_on_terminate"`

	// SkipLeaveOnInt controls if Serf skips a graceful leave when receiving
	// the INT signal. Defaults false. This can be changed on reload.
	SkipLeaveOnInt bool `mapstructure:"skip_leave_on_interrupt"`

	// StatsiteAddr is the address of a statsite instance. If provided,
	// metrics will be streamed to that instance.
	StatsiteAddr string `mapstructure:"statsite_addr"`

	// StatsdAddr is the address of a statsd instance. If provided,
	// metrics will be sent to that instance.
	StatsdAddr string `mapstructure:"statsd_addr"`

	// Protocol is the Consul protocol version to use.
	Protocol int `mapstructure:"protocol"`

	// EnableDebug is used to enable various debugging features
	EnableDebug bool `mapstructure:"enable_debug"`

	// VerifyIncoming is used to verify the authenticity of incoming connections.
	// This means that TCP requests are forbidden, only allowing for TLS. TLS connections
	// must match a provided certificate authority. This can be used to force client auth.
	VerifyIncoming bool `mapstructure:"verify_incoming"`

	// VerifyOutgoing is used to verify the authenticity of outgoing connections.
	// This means that TLS requests are used. TLS connections must match a provided
	// certificate authority. This is used to verify authenticity of server nodes.
	VerifyOutgoing bool `mapstructure:"verify_outgoing"`

	// CAFile is a path to a certificate authority file. This is used with VerifyIncoming
	// or VerifyOutgoing to verify the TLS connection.
	CAFile string `mapstructure:"ca_file"`

	// CertFile is used to provide a TLS certificate that is used for serving TLS connections.
	// Must be provided to serve TLS connections.
	CertFile string `mapstructure:"cert_file"`

	// KeyFile is used to provide a TLS key that is used for serving TLS connections.
	// Must be provided to serve TLS connections.
	KeyFile string `mapstructure:"key_file"`

	// ServerName is used with the TLS certificates to ensure the name we
	// provid ematches the certificate
	ServerName string `mapstructure:"server_name"`

	// StartJoin is a list of addresses to attempt to join when the
	// agent starts. If Serf is unable to communicate with any of these
	// addresses, then the agent will error and exit.
	StartJoin []string `mapstructure:"start_join"`

	// StartJoinWan is a list of addresses to attempt to join -wan when the
	// agent starts. If Serf is unable to communicate with any of these
	// addresses, then the agent will error and exit.
	StartJoinWan []string `mapstructure:"start_join_wan"`

	// RetryJoin is a list of addresses to join with retry enabled.
	RetryJoin []string `mapstructure:"retry_join"`

	// RetryMaxAttempts specifies the maximum number of times to retry joining a
	// host on startup. This is useful for cases where we know the node will be
	// online eventually.
	RetryMaxAttempts int `mapstructure:"retry_max"`

	// RetryInterval specifies the amount of time to wait in between join
	// attempts on agent start. The minimum allowed value is 1 second and
	// the default is 30s.
	RetryInterval    time.Duration `mapstructure:"-" json:"-"`
	RetryIntervalRaw string        `mapstructure:"retry_interval"`

	// RetryJoinWan is a list of addresses to join -wan with retry enabled.
	RetryJoinWan []string `mapstructure:"retry_join_wan"`

	// RetryMaxAttemptsWan specifies the maximum number of times to retry joining a
	// -wan host on startup. This is useful for cases where we know the node will be
	// online eventually.
	RetryMaxAttemptsWan int `mapstructure:"retry_max_wan"`

	// RetryIntervalWan specifies the amount of time to wait in between join
	// -wan attempts on agent start. The minimum allowed value is 1 second and
	// the default is 30s.
	RetryIntervalWan    time.Duration `mapstructure:"-" json:"-"`
	RetryIntervalWanRaw string        `mapstructure:"retry_interval_wan"`

	// UiDir is the directory containing the Web UI resources.
	// If provided, the UI endpoints will be enabled.
	UiDir string `mapstructure:"ui_dir"`

	// PidFile is the file to store our PID in
	PidFile string `mapstructure:"pid_file"`

	// EnableSyslog is used to also tee all the logs over to syslog. Only supported
	// on linux and OSX. Other platforms will generate an error.
	EnableSyslog bool `mapstructure:"enable_syslog"`

	// SyslogFacility is used to control where the syslog messages go
	// By default, goes to LOCAL0
	SyslogFacility string `mapstructure:"syslog_facility"`

	// RejoinAfterLeave controls our interaction with the cluster after leave.
	// When set to false (default), a leave causes Consul to not rejoin
	// the cluster until an explicit join is received. If this is set to
	// true, we ignore the leave, and rejoin the cluster on start.
	RejoinAfterLeave bool `mapstructure:"rejoin_after_leave"`

	// CheckUpdateInterval controls the interval on which the output of a health check
	// is updated if there is no change to the state. For example, a check in a steady
	// state may run every 5 second generating a unique output (timestamp, etc), forcing
	// constant writes. This allows Consul to defer the write for some period of time,
	// reducing the write pressure when the state is steady.
	CheckUpdateInterval    time.Duration `mapstructure:"-"`
	CheckUpdateIntervalRaw string        `mapstructure:"check_update_interval" json:"-"`

	// ACLToken is the default token used to make requests if a per-request
	// token is not provided. If not configured the 'anonymous' token is used.
	ACLToken string `mapstructure:"acl_token" json:"-"`

	// ACLMasterToken is used to bootstrap the ACL system. It should be specified
	// on the servers in the ACLDatacenter. When the leader comes online, it ensures
	// that the Master token is available. This provides the initial token.
	ACLMasterToken string `mapstructure:"acl_master_token" json:"-"`

	// ACLDatacenter is the central datacenter that holds authoritative
	// ACL records. This must be the same for the entire cluster.
	// If this is not set, ACLs are not enabled. Off by default.
	ACLDatacenter string `mapstructure:"acl_datacenter"`

	// ACLTTL is used to control the time-to-live of cached ACLs . This has
	// a major impact on performance. By default, it is set to 30 seconds.
	ACLTTL    time.Duration `mapstructure:"-"`
	ACLTTLRaw string        `mapstructure:"acl_ttl"`

	// ACLDefaultPolicy is used to control the ACL interaction when
	// there is no defined policy. This can be "allow" which means
	// ACLs are used to black-list, or "deny" which means ACLs are
	// white-lists.
	ACLDefaultPolicy string `mapstructure:"acl_default_policy"`

	// ACLDownPolicy is used to control the ACL interaction when we cannot
	// reach the ACLDatacenter and the token is not in the cache.
	// There are two modes:
	//   * deny - Deny all requests
	//   * extend-cache - Ignore the cache expiration, and allow cached
	//                    ACL's to be used to service requests. This
	//	                  is the default. If the ACL is not in the cache,
	//                    this acts like deny.
	ACLDownPolicy string `mapstructure:"acl_down_policy"`

	// Watches are used to monitor various endpoints and to invoke a
	// handler to act appropriately. These are managed entirely in the
	// agent layer using the standard APIs.
	Watches []map[string]interface{} `mapstructure:"watches"`

	// DisableRemoteExec is used to turn off the remote execution
	// feature. This is for security to prevent unknown scripts from running.
	DisableRemoteExec bool `mapstructure:"disable_remote_exec"`

	// DisableUpdateCheck is used to turn off the automatic update and
	// security bulletin checking.
	DisableUpdateCheck bool `mapstructure:"disable_update_check"`

	// DisableAnonymousSignature is used to turn off the anonymous signature
	// send with the update check. This is used to deduplicate messages.
	DisableAnonymousSignature bool `mapstructure:"disable_anonymous_signature"`

	// HTTPAPIResponseHeaders are used to add HTTP header response fields to the HTTP API responses.
	HTTPAPIResponseHeaders map[string]string `mapstructure:"http_api_response_headers"`

	// AEInterval controls the anti-entropy interval. This is how often
	// the agent attempts to reconcile it's local state with the server'
	// representation of our state. Defaults to every 60s.
	AEInterval time.Duration `mapstructure:"-" json:"-"`

	// Checks holds the provided check definitions
	Checks []*CheckDefinition `mapstructure:"-" json:"-"`

	// Services holds the provided service definitions
	Services []*ServiceDefinition `mapstructure:"-" json:"-"`

	// ConsulConfig can either be provided or a default one created
	ConsulConfig *consul.Config `mapstructure:"-" json:"-"`

	// Revision is the GitCommit this maps to
	Revision string `mapstructure:"-"`

	// Version is the release version number
	Version string `mapstructure:"-"`

	// VersionPrerelease is a label for pre-release builds
	VersionPrerelease string `mapstructure:"-"`

	// WatchPlans contains the compiled watches
	WatchPlans []*watch.WatchPlan `mapstructure:"-" json:"-"`
}

type dirEnts []os.FileInfo

// DefaultConfig is used to return a sane default configuration
func DefaultConfig() *Config {
	return &Config{
		Bootstrap:       false,
		BootstrapExpect: 0,
		Server:          false,
		Datacenter:      consul.DefaultDC,
		Domain:          "consul.",
		LogLevel:        "INFO",
		ClientAddr:      "127.0.0.1",
		BindAddr:        "0.0.0.0",
		Ports: PortConfig{
			DNS:     8600,
			HTTP:    8500,
			HTTPS:   -1,
			RPC:     8400,
			SerfLan: consul.DefaultLANSerfPort,
			SerfWan: consul.DefaultWANSerfPort,
			Server:  8300,
		},
		DNSConfig: DNSConfig{
			MaxStale: 5 * time.Second,
		},
		SyslogFacility:      "LOCAL0",
		Protocol:            consul.ProtocolVersionMax,
		CheckUpdateInterval: 5 * time.Minute,
		AEInterval:          time.Minute,
		ACLTTL:              30 * time.Second,
		ACLDownPolicy:       "extend-cache",
		ACLDefaultPolicy:    "allow",
		RetryInterval:       30 * time.Second,
		RetryIntervalWan:    30 * time.Second,
	}
}

// EncryptBytes returns the encryption key configured.
func (c *Config) EncryptBytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(c.EncryptKey)
}

// ClientListener is used to format a listener for a
// port on a ClientAddr
func (c *Config) ClientListener(override string, port int) (*net.TCPAddr, error) {
	var addr string
	if override != "" {
		addr = override
	} else {
		addr = c.ClientAddr
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return nil, fmt.Errorf("Failed to parse IP: %v", addr)
	}
	return &net.TCPAddr{IP: ip, Port: port}, nil
}

// ClientListenerAddr is used to format an address for a
// port on a ClientAddr, handling the zero IP.
func (c *Config) ClientListenerAddr(override string, port int) (string, error) {
	addr, err := c.ClientListener(override, port)
	if err != nil {
		return "", err
	}
	if addr.IP.IsUnspecified() {
		addr.IP = net.ParseIP("127.0.0.1")
	}
	return addr.String(), nil
}

// DecodeConfig reads the configuration from the given reader in JSON
// format and decodes it into a proper Config structure.
func DecodeConfig(r io.Reader) (*Config, error) {
	var raw interface{}
	var result Config
	dec := json.NewDecoder(r)
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}

	// Check the result type
	if obj, ok := raw.(map[string]interface{}); ok {
		// Check for a "services", "service" or "check" key, meaning
		// this is actually a definition entry
		if sub, ok := obj["services"]; ok {
			if list, ok := sub.([]interface{}); ok {
				for _, srv := range list {
					service, err := DecodeServiceDefinition(srv)
					if err != nil {
						return nil, err
					}
					result.Services = append(result.Services, service)
				}
			}
		}
		if sub, ok := obj["service"]; ok {
			service, err := DecodeServiceDefinition(sub)
			if err != nil {
				return nil, err
			}
			result.Services = append(result.Services, service)
		}
		if sub, ok := obj["checks"]; ok {
			if list, ok := sub.([]interface{}); ok {
				for _, chk := range list {
					check, err := DecodeCheckDefinition(chk)
					if err != nil {
						return nil, err
					}
					result.Checks = append(result.Checks, check)
				}
			}
		}
		if sub, ok := obj["check"]; ok {
			check, err := DecodeCheckDefinition(sub)
			if err != nil {
				return nil, err
			}
			result.Checks = append(result.Checks, check)
		}
	}

	// Decode
	var md mapstructure.Metadata
	msdec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Metadata: &md,
		Result:   &result,
	})
	if err != nil {
		return nil, err
	}

	if err := msdec.Decode(raw); err != nil {
		return nil, err
	}

	// Check unused fields and verify that no bad configuration options were
	// passed to Consul. There are a few additional fields which don't directly
	// use mapstructure decoding, so we need to account for those as well.
	allowedKeys := []string{"service", "services", "check", "checks"}
	var unused []string
	for _, field := range md.Unused {
		if !strContains(allowedKeys, field) {
			unused = append(unused, field)
		}
	}
	if len(unused) > 0 {
		return nil, fmt.Errorf("Config has invalid keys: %s", strings.Join(unused, ","))
	}

	// Handle time conversions
	if raw := result.DNSConfig.NodeTTLRaw; raw != "" {
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("NodeTTL invalid: %v", err)
		}
		result.DNSConfig.NodeTTL = dur
	}

	if raw := result.DNSConfig.MaxStaleRaw; raw != "" {
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("MaxStale invalid: %v", err)
		}
		result.DNSConfig.MaxStale = dur
	}

	if len(result.DNSConfig.ServiceTTLRaw) != 0 {
		if result.DNSConfig.ServiceTTL == nil {
			result.DNSConfig.ServiceTTL = make(map[string]time.Duration)
		}
		for service, raw := range result.DNSConfig.ServiceTTLRaw {
			dur, err := time.ParseDuration(raw)
			if err != nil {
				return nil, fmt.Errorf("ServiceTTL %s invalid: %v", service, err)
			}
			result.DNSConfig.ServiceTTL[service] = dur
		}
	}

	if raw := result.CheckUpdateIntervalRaw; raw != "" {
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("CheckUpdateInterval invalid: %v", err)
		}
		result.CheckUpdateInterval = dur
	}

	if raw := result.ACLTTLRaw; raw != "" {
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("ACL TTL invalid: %v", err)
		}
		result.ACLTTL = dur
	}

	if raw := result.RetryIntervalRaw; raw != "" {
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("RetryInterval invalid: %v", err)
		}
		result.RetryInterval = dur
	}

	if raw := result.RetryIntervalWanRaw; raw != "" {
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("RetryIntervalWan invalid: %v", err)
		}
		result.RetryIntervalWan = dur
	}

	// Merge the single recursor
	if result.DNSRecursor != "" {
		result.DNSRecursors = append(result.DNSRecursors, result.DNSRecursor)
	}

	return &result, nil
}

// DecodeServiceDefinition is used to decode a service definition
func DecodeServiceDefinition(raw interface{}) (*ServiceDefinition, error) {
	var sub interface{}
	rawMap, ok := raw.(map[string]interface{})
	if !ok {
		goto AFTER_FIX
	}

	// If no 'tags', handle the deprecated 'tag' value.
	if _, ok := rawMap["tags"]; !ok {
		if tag, ok := rawMap["tag"]; ok {
			rawMap["tags"] = []interface{}{tag}
		}
	}

	for k, v := range rawMap {
		if strings.ToLower(k) == "check" {
			sub = v
			break
		}
	}
	if sub == nil {
		goto AFTER_FIX
	}
	if err := FixupCheckType(sub); err != nil {
		return nil, err
	}
AFTER_FIX:
	var md mapstructure.Metadata
	var result ServiceDefinition
	msdec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Metadata: &md,
		Result:   &result,
	})
	if err != nil {
		return nil, err
	}
	if err := msdec.Decode(raw); err != nil {
		return nil, err
	}
	return &result, nil
}

func FixupCheckType(raw interface{}) error {
	// Handle decoding of time durations
	rawMap, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}

	var ttlKey string
	for k, _ := range rawMap {
		if strings.ToLower(k) == "ttl" {
			ttlKey = k
		}
	}
	var intervalKey string
	for k, _ := range rawMap {
		if strings.ToLower(k) == "interval" {
			intervalKey = k
		}
	}

	if ttl, ok := rawMap[ttlKey]; ok {
		ttlS, ok := ttl.(string)
		if ok {
			if dur, err := time.ParseDuration(ttlS); err != nil {
				return err
			} else {
				rawMap[ttlKey] = dur
			}
		}
	}

	if interval, ok := rawMap[intervalKey]; ok {
		intervalS, ok := interval.(string)
		if ok {
			if dur, err := time.ParseDuration(intervalS); err != nil {
				return err
			} else {
				rawMap[intervalKey] = dur
			}
		}
	}
	return nil
}

// DecodeCheckDefinition is used to decode a check definition
func DecodeCheckDefinition(raw interface{}) (*CheckDefinition, error) {
	if err := FixupCheckType(raw); err != nil {
		return nil, err
	}
	var md mapstructure.Metadata
	var result CheckDefinition
	msdec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Metadata: &md,
		Result:   &result,
	})
	if err != nil {
		return nil, err
	}
	if err := msdec.Decode(raw); err != nil {
		return nil, err
	}
	return &result, nil
}

// MergeConfig merges two configurations together to make a single new
// configuration.
func MergeConfig(a, b *Config) *Config {
	var result Config = *a

	// Copy the strings if they're set
	if b.Bootstrap {
		result.Bootstrap = true
	}
	if b.BootstrapExpect != 0 {
		result.BootstrapExpect = b.BootstrapExpect
	}
	if b.Datacenter != "" {
		result.Datacenter = b.Datacenter
	}
	if b.DataDir != "" {
		result.DataDir = b.DataDir
	}

	// Copy the dns recursors
	result.DNSRecursors = make([]string, 0, len(a.DNSRecursors)+len(b.DNSRecursors))
	result.DNSRecursors = append(result.DNSRecursors, a.DNSRecursors...)
	result.DNSRecursors = append(result.DNSRecursors, b.DNSRecursors...)

	if b.Domain != "" {
		result.Domain = b.Domain
	}
	if b.EncryptKey != "" {
		result.EncryptKey = b.EncryptKey
	}
	if b.LogLevel != "" {
		result.LogLevel = b.LogLevel
	}
	if b.Protocol > 0 {
		result.Protocol = b.Protocol
	}
	if b.NodeName != "" {
		result.NodeName = b.NodeName
	}
	if b.ClientAddr != "" {
		result.ClientAddr = b.ClientAddr
	}
	if b.BindAddr != "" {
		result.BindAddr = b.BindAddr
	}
	if b.AdvertiseAddr != "" {
		result.AdvertiseAddr = b.AdvertiseAddr
	}
	if b.Server == true {
		result.Server = b.Server
	}
	if b.LeaveOnTerm == true {
		result.LeaveOnTerm = true
	}
	if b.SkipLeaveOnInt == true {
		result.SkipLeaveOnInt = true
	}
	if b.StatsiteAddr != "" {
		result.StatsiteAddr = b.StatsiteAddr
	}
	if b.StatsdAddr != "" {
		result.StatsdAddr = b.StatsdAddr
	}
	if b.EnableDebug {
		result.EnableDebug = true
	}
	if b.VerifyIncoming {
		result.VerifyIncoming = true
	}
	if b.VerifyOutgoing {
		result.VerifyOutgoing = true
	}
	if b.CAFile != "" {
		result.CAFile = b.CAFile
	}
	if b.CertFile != "" {
		result.CertFile = b.CertFile
	}
	if b.KeyFile != "" {
		result.KeyFile = b.KeyFile
	}
	if b.ServerName != "" {
		result.ServerName = b.ServerName
	}
	if b.Checks != nil {
		result.Checks = append(result.Checks, b.Checks...)
	}
	if b.Services != nil {
		result.Services = append(result.Services, b.Services...)
	}
	if b.Ports.DNS != 0 {
		result.Ports.DNS = b.Ports.DNS
	}
	if b.Ports.HTTP != 0 {
		result.Ports.HTTP = b.Ports.HTTP
	}
	if b.Ports.HTTPS != 0 {
		result.Ports.HTTPS = b.Ports.HTTPS
	}
	if b.Ports.RPC != 0 {
		result.Ports.RPC = b.Ports.RPC
	}
	if b.Ports.SerfLan != 0 {
		result.Ports.SerfLan = b.Ports.SerfLan
	}
	if b.Ports.SerfWan != 0 {
		result.Ports.SerfWan = b.Ports.SerfWan
	}
	if b.Ports.Server != 0 {
		result.Ports.Server = b.Ports.Server
	}
	if b.Addresses.DNS != "" {
		result.Addresses.DNS = b.Addresses.DNS
	}
	if b.Addresses.HTTP != "" {
		result.Addresses.HTTP = b.Addresses.HTTP
	}
	if b.Addresses.HTTPS != "" {
		result.Addresses.HTTPS = b.Addresses.HTTPS
	}
	if b.Addresses.RPC != "" {
		result.Addresses.RPC = b.Addresses.RPC
	}
	if b.UiDir != "" {
		result.UiDir = b.UiDir
	}
	if b.PidFile != "" {
		result.PidFile = b.PidFile
	}
	if b.EnableSyslog {
		result.EnableSyslog = true
	}
	if b.RejoinAfterLeave {
		result.RejoinAfterLeave = true
	}
	if b.RetryMaxAttempts != 0 {
		result.RetryMaxAttempts = b.RetryMaxAttempts
	}
	if b.RetryInterval != 0 {
		result.RetryInterval = b.RetryInterval
	}
	if b.RetryMaxAttemptsWan != 0 {
		result.RetryMaxAttemptsWan = b.RetryMaxAttemptsWan
	}
	if b.RetryIntervalWan != 0 {
		result.RetryIntervalWan = b.RetryIntervalWan
	}
	if b.DNSConfig.NodeTTL != 0 {
		result.DNSConfig.NodeTTL = b.DNSConfig.NodeTTL
	}
	if len(b.DNSConfig.ServiceTTL) != 0 {
		if result.DNSConfig.ServiceTTL == nil {
			result.DNSConfig.ServiceTTL = make(map[string]time.Duration)
		}
		for service, dur := range b.DNSConfig.ServiceTTL {
			result.DNSConfig.ServiceTTL[service] = dur
		}
	}
	if b.DNSConfig.AllowStale {
		result.DNSConfig.AllowStale = true
	}
	if b.DNSConfig.EnableTruncate {
		result.DNSConfig.EnableTruncate = true
	}
	if b.DNSConfig.MaxStale != 0 {
		result.DNSConfig.MaxStale = b.DNSConfig.MaxStale
	}
	if b.DNSConfig.OnlyPassing {
		result.DNSConfig.OnlyPassing = true
	}
	if b.CheckUpdateIntervalRaw != "" || b.CheckUpdateInterval != 0 {
		result.CheckUpdateInterval = b.CheckUpdateInterval
	}
	if b.SyslogFacility != "" {
		result.SyslogFacility = b.SyslogFacility
	}
	if b.ACLToken != "" {
		result.ACLToken = b.ACLToken
	}
	if b.ACLMasterToken != "" {
		result.ACLMasterToken = b.ACLMasterToken
	}
	if b.ACLDatacenter != "" {
		result.ACLDatacenter = b.ACLDatacenter
	}
	if b.ACLTTLRaw != "" {
		result.ACLTTL = b.ACLTTL
		result.ACLTTLRaw = b.ACLTTLRaw
	}
	if b.ACLDownPolicy != "" {
		result.ACLDownPolicy = b.ACLDownPolicy
	}
	if b.ACLDefaultPolicy != "" {
		result.ACLDefaultPolicy = b.ACLDefaultPolicy
	}
	if len(b.Watches) != 0 {
		result.Watches = append(result.Watches, b.Watches...)
	}
	if len(b.WatchPlans) != 0 {
		result.WatchPlans = append(result.WatchPlans, b.WatchPlans...)
	}
	if b.DisableRemoteExec {
		result.DisableRemoteExec = true
	}
	if b.DisableUpdateCheck {
		result.DisableUpdateCheck = true
	}
	if b.DisableAnonymousSignature {
		result.DisableAnonymousSignature = true
	}

	if len(b.HTTPAPIResponseHeaders) != 0 {
		if result.HTTPAPIResponseHeaders == nil {
			result.HTTPAPIResponseHeaders = make(map[string]string)
		}
		for field, value := range b.HTTPAPIResponseHeaders {
			result.HTTPAPIResponseHeaders[field] = value
		}
	}

	// Copy the start join addresses
	result.StartJoin = make([]string, 0, len(a.StartJoin)+len(b.StartJoin))
	result.StartJoin = append(result.StartJoin, a.StartJoin...)
	result.StartJoin = append(result.StartJoin, b.StartJoin...)

	// Copy the start join addresses
	result.StartJoinWan = make([]string, 0, len(a.StartJoinWan)+len(b.StartJoinWan))
	result.StartJoinWan = append(result.StartJoinWan, a.StartJoinWan...)
	result.StartJoinWan = append(result.StartJoinWan, b.StartJoinWan...)

	// Copy the retry join addresses
	result.RetryJoin = make([]string, 0, len(a.RetryJoin)+len(b.RetryJoin))
	result.RetryJoin = append(result.RetryJoin, a.RetryJoin...)
	result.RetryJoin = append(result.RetryJoin, b.RetryJoin...)

	// Copy the retry join -wan addresses
	result.RetryJoinWan = make([]string, 0, len(a.RetryJoinWan)+len(b.RetryJoinWan))
	result.RetryJoinWan = append(result.RetryJoinWan, a.RetryJoinWan...)
	result.RetryJoinWan = append(result.RetryJoinWan, b.RetryJoinWan...)

	return &result
}

// ReadConfigPaths reads the paths in the given order to load configurations.
// The paths can be to files or directories. If the path is a directory,
// we read one directory deep and read any files ending in ".json" as
// configuration files.
func ReadConfigPaths(paths []string) (*Config, error) {
	result := new(Config)
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("Error reading '%s': %s", path, err)
		}

		fi, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("Error reading '%s': %s", path, err)
		}

		if !fi.IsDir() {
			config, err := DecodeConfig(f)
			f.Close()

			if err != nil {
				return nil, fmt.Errorf("Error decoding '%s': %s", path, err)
			}

			result = MergeConfig(result, config)
			continue
		}

		contents, err := f.Readdir(-1)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("Error reading '%s': %s", path, err)
		}

		// Sort the contents, ensures lexical order
		sort.Sort(dirEnts(contents))

		for _, fi := range contents {
			// Don't recursively read contents
			if fi.IsDir() {
				continue
			}

			// If it isn't a JSON file, ignore it
			if !strings.HasSuffix(fi.Name(), ".json") {
				continue
			}

			subpath := filepath.Join(path, fi.Name())
			f, err := os.Open(subpath)
			if err != nil {
				return nil, fmt.Errorf("Error reading '%s': %s", subpath, err)
			}

			config, err := DecodeConfig(f)
			f.Close()

			if err != nil {
				return nil, fmt.Errorf("Error decoding '%s': %s", subpath, err)
			}

			result = MergeConfig(result, config)
		}
	}

	return result, nil
}

// Implement the sort interface for dirEnts
func (d dirEnts) Len() int {
	return len(d)
}

func (d dirEnts) Less(i, j int) bool {
	return d[i].Name() < d[j].Name()
}

func (d dirEnts) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}
