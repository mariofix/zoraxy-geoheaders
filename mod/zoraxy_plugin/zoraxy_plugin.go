package zoraxy_plugin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

/*
	Zoraxy Plugin Protocol Definitions and Helper Functions
*/

type PluginType int

const (
	PluginType_Router    PluginType = 0 // Router Plugin, used for handling / routing / forwarding traffic
	PluginType_Utilities PluginType = 1 // Utilities Plugin, used for utilities like Zerotier or Static Web Server
)

const PluginTypeRouter = PluginType_Router
const PluginTypeUtilities = PluginType_Utilities

type StaticCaptureRule struct {
	CapturePath string `json:"capture_path"`
}

type ControlStatusCode int

const (
	ControlStatusCode_CAPTURED  ControlStatusCode = 280 // Traffic captured by plugin, ask Zoraxy not to process the traffic
	ControlStatusCode_UNHANDLED ControlStatusCode = 284 // Traffic not handled by plugin, ask Zoraxy to process the traffic
	ControlStatusCode_ERROR     ControlStatusCode = 580 // Error occurred while processing the traffic
)

type SubscriptionEvent struct {
	EventName   string `json:"event_name"`
	EventSource string `json:"event_source"`
	Payload     string `json:"payload"`
}

type RuntimeConstantValue struct {
	ZoraxyVersion    string `json:"zoraxy_version"`
	ZoraxyUUID       string `json:"zoraxy_uuid"`
	DevelopmentBuild bool   `json:"development_build"`
}

type PermittedAPIEndpoint struct {
	Method   string `json:"method"`   // HTTP method for the API endpoint (e.g. GET, POST)
	Endpoint string `json:"endpoint"` // The API endpoint that the plugin can access
	Reason   string `json:"reason"`   // The reason why the plugin needs to access this endpoint
}

/*
IntroSpect Payload

When the plugin is initialized with -introspect flag,
the plugin shall return this payload as JSON and exit
*/
type IntroSpect struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Author        string     `json:"author"`
	AuthorContact string     `json:"author_contact,omitempty"`
	Description   string     `json:"description"`
	URL           string     `json:"url,omitempty"`
	Type          PluginType `json:"type"`
	VersionMajor  int        `json:"version_major"`
	VersionMinor  int        `json:"version_minor"`
	VersionPatch  int        `json:"version_patch"`

	StaticCapturePaths   []StaticCaptureRule `json:"static_capture_paths,omitempty"`
	StaticCaptureIngress string              `json:"static_capture_ingress,omitempty"`

	DynamicCaptureSniff   string `json:"dynamic_capture_sniff,omitempty"`
	DynamicCaptureIngress string `json:"dynamic_capture_ingress,omitempty"`

	UIPath string `json:"ui_path,omitempty"`

	SubscriptionPath    string            `json:"subscription_path,omitempty"`
	SubscriptionsEvents map[string]string `json:"subscriptions_events,omitempty"`

	PermittedAPIEndpoints []PermittedAPIEndpoint `json:"permitted_api_endpoints,omitempty"`
}

type IntrospectSpec struct {
	Name               string     `json:"Name"`
	Author             string     `json:"Author"`
	Version            string     `json:"Version"`
	Type               PluginType `json:"Type"`
	TargetTag          string     `json:"TargetTag"`
	Description        string     `json:"Description"`
	DynamicRouter      bool       `json:"DynamicRouter"`
	AllowConfiguration bool       `json:"AllowConfiguration"`
}

func HandleIntrospectFlag(spec IntrospectSpec) {
	jsonData, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal introspect: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(jsonData))
}

func ServeIntroSpect(pluginSpect *IntroSpect) {
	if len(os.Args) > 1 && os.Args[1] == "-introspect" {
		jsonData, err := json.MarshalIndent(pluginSpect, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to marshal introspect: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(jsonData))
		os.Exit(0)
	}
}

type ConfigureSpec struct {
	Port          int                  `json:"port,omitempty"`
	ListeningPort int                  `json:"listening_port,omitempty"`
	WebRootPath   string               `json:"web_root_path,omitempty"`
	RuntimeFolder string               `json:"runtime_folder,omitempty"`
	RuntimeConst  RuntimeConstantValue `json:"runtime_const,omitempty"`
	APIKey        string               `json:"api_key,omitempty"`
	ZoraxyPort    int                  `json:"zoraxy_port,omitempty"`
}

func ParseConfigureFlag(payload string) (*ConfigureSpec, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, fmt.Errorf("empty configure payload")
	}

	var data []byte
	// Try base64 decode first
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err == nil && len(decoded) > 0 && (decoded[0] == '{' || decoded[0] == '[') {
		data = decoded
	} else {
		data = []byte(payload)
	}

	var configSpec ConfigureSpec
	if err := json.Unmarshal(data, &configSpec); err != nil {
		return nil, err
	}
	if configSpec.ListeningPort == 0 && configSpec.Port > 0 {
		configSpec.ListeningPort = configSpec.Port
	}
	return &configSpec, nil
}

func RecvConfigureSpec() (*ConfigureSpec, error) {
	for i, arg := range os.Args {
		if strings.HasPrefix(arg, "-configure=") {
			return ParseConfigureFlag(arg[11:])
		} else if arg == "-configure" {
			if len(os.Args) > i+1 {
				return ParseConfigureFlag(os.Args[i+1])
			}
			return nil, fmt.Errorf("no config payload specified after -configure flag")
		}
	}
	return nil, fmt.Errorf("no -configure flag found")
}

func ServeAndRecvSpec(pluginSpect *IntroSpect) (*ConfigureSpec, error) {
	ServeIntroSpect(pluginSpect)
	return RecvConfigureSpec()
}

