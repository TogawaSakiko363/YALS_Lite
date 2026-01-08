package config

import (
	"fmt"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen struct {
		Host        string `yaml:"host"`
		Port        int    `yaml:"port"`
		LogLevel    string `yaml:"log_level"`
		TLS         bool   `yaml:"tls"`
		TLSCertFile string `yaml:"tls_cert_file"`
		TLSKeyFile  string `yaml:"tls_key_file"`
	} `yaml:"listen"`

	RateLimit struct {
		Enabled     bool `yaml:"enabled"`
		MaxCommands int  `yaml:"max_commands"`
		TimeWindow  int  `yaml:"time_window"`
	} `yaml:"rate_limit"`

	Info struct {
		Name        string `yaml:"name"`
		Location    string `yaml:"location"`
		Datacenter  string `yaml:"datacenter"`
		TestIP      string `yaml:"test_ip"`
		Description string `yaml:"description"`
	} `yaml:"info"`

	Commands map[string]CommandTemplate `yaml:"commands"`
}

type CommandTemplate struct {
	Template     string `yaml:"template"`
	Description  string `yaml:"description"`
	IgnoreTarget bool   `yaml:"ignore_target"`
	MaximumQueue int    `yaml:"maxmium_queue"`
}

type CommandInfo struct {
	Name         string `json:"name"`
	Template     string `json:"template"`
	Description  string `json:"description"`
	IgnoreTarget bool   `json:"ignore_target"`
	MaximumQueue int    `json:"maxmium_queue"`
}

type commandWithLine struct {
	Name    string
	Line    int
	Command CommandTemplate
}

var globalConfig *Config
var commandOrder []string

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var rawConfig struct {
		Commands map[string]CommandTemplate `yaml:"commands"`
	}

	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return nil, fmt.Errorf("error parsing config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing config file: %w", err)
	}

	commandOrder = extractCommandOrder(data)

	if config.Commands == nil {
		config.Commands = make(map[string]CommandTemplate)
	}
	if config.Info.Location == "" {
		config.Info.Location = "N/A"
	}
	if config.Info.Datacenter == "" {
		config.Info.Datacenter = "N/A"
	}
	if config.Info.TestIP == "" {
		config.Info.TestIP = "N/A"
	}
	if config.Info.Description == "" {
		config.Info.Description = "N/A"
	}

	globalConfig = &config

	return &config, nil
}

func extractCommandOrder(data []byte) []string {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil
	}

	var commands []commandWithLine
	extractCommandsFromNode(&node, &commands)

	slices.SortFunc(commands, func(a, b commandWithLine) int {
		return a.Line - b.Line
	})

	names := make([]string, len(commands))
	for i, cmd := range commands {
		names[i] = cmd.Name
	}

	return names
}

func extractCommandsFromNode(node *yaml.Node, commands *[]commandWithLine) {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		extractCommandsFromNode(node.Content[0], commands)
		return
	}

	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]

			// Found the "commands" key
			if keyNode.Value == "commands" && valueNode.Kind == yaml.MappingNode {
				// Iterate through each command in the commands map
				for j := 0; j < len(valueNode.Content); j += 2 {
					cmdNameNode := valueNode.Content[j]
					cmdValueNode := valueNode.Content[j+1]

					if cmdNameNode.Kind == yaml.ScalarNode && cmdValueNode.Kind == yaml.MappingNode {
						cmdName := cmdNameNode.Value
						var cmdTemplate CommandTemplate

						// Parse command properties
						for k := 0; k < len(cmdValueNode.Content); k += 2 {
							propKey := cmdValueNode.Content[k].Value
							propValue := cmdValueNode.Content[k+1]

							switch propKey {
							case "template":
								cmdTemplate.Template = propValue.Value
							case "description":
								cmdTemplate.Description = propValue.Value
							case "ignore_target":
								cmdTemplate.IgnoreTarget = propValue.Value == "true"
							case "maxmium_queue":
								fmt.Sscanf(propValue.Value, "%d", &cmdTemplate.MaximumQueue)
							}
						}

						*commands = append(*commands, commandWithLine{
							Name:    cmdName,
							Line:    cmdNameNode.Line,
							Command: cmdTemplate,
						})
					}
				}
				return
			}
		}
	}
}

func GetConfig() *Config {
	return globalConfig
}

type ServerInfo struct {
	cfg *Config
}

func NewServerInfo(cfg *Config) *ServerInfo {
	return &ServerInfo{cfg: cfg}
}

func (s *ServerInfo) GetCommandConfig(commandName string) (CommandTemplate, bool) {
	if template, exists := s.cfg.Commands[commandName]; exists {
		return template, true
	}
	return CommandTemplate{}, false
}

func (s *ServerInfo) GetCommands() []CommandInfo {
	commandsMap := s.cfg.Commands
	commands := make([]CommandInfo, 0, len(commandOrder))

	for _, name := range commandOrder {
		if template, exists := commandsMap[name]; exists {
			commands = append(commands, CommandInfo{
				Name:         name,
				Template:     template.Template,
				Description:  template.Description,
				IgnoreTarget: template.IgnoreTarget,
				MaximumQueue: template.MaximumQueue,
			})
		}
	}

	unorderedNames := []string{}
	for name := range commandsMap {
		if !slices.Contains(commandOrder, name) {
			unorderedNames = append(unorderedNames, name)
		}
	}
	slices.Sort(unorderedNames)

	for _, name := range unorderedNames {
		template := commandsMap[name]
		commands = append(commands, CommandInfo{
			Name:         name,
			Template:     template.Template,
			Description:  template.Description,
			IgnoreTarget: template.IgnoreTarget,
			MaximumQueue: template.MaximumQueue,
		})
	}

	return commands
}

func (s *ServerInfo) GetInfo() map[string]interface{} {
	return map[string]interface{}{
		"name":        s.cfg.Info.Name,
		"location":    s.cfg.Info.Location,
		"datacenter":  s.cfg.Info.Datacenter,
		"test_ip":     s.cfg.Info.TestIP,
		"description": s.cfg.Info.Description,
	}
}
