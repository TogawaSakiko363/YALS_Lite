package config

import (
	"fmt"
	"os"
	"slices"
	"strings"

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
	for i := 0; i < len(node.Content); i++ {
		child := node.Content[i]

		if child.Kind == yaml.MappingNode {
			var key string
			for j := 0; j < len(child.Content); j += 2 {
				if child.Content[j].Kind == yaml.ScalarNode {
					key = child.Content[j].Value
					break
				}
			}

			if strings.HasPrefix(key, "commands:") || (key == "commands" && len(child.Content) > 1) {
				for _, cmdNode := range child.Content {
					if cmdNode.Kind == yaml.MappingNode {
						cmdName := ""
						var cmdTemplate CommandTemplate
						for k := 0; k < len(cmdNode.Content); k += 2 {
							if cmdNode.Content[k].Kind == yaml.ScalarNode {
								fieldName := cmdNode.Content[k].Value
								if fieldName == "template" && k+1 < len(cmdNode.Content) {
									cmdTemplate.Template = cmdNode.Content[k+1].Value
								} else if fieldName == "description" && k+1 < len(cmdNode.Content) {
									cmdTemplate.Description = cmdNode.Content[k+1].Value
								} else if fieldName == "ignore_target" && k+1 < len(cmdNode.Content) {
									cmdTemplate.IgnoreTarget = cmdNode.Content[k+1].Value == "true"
								} else if fieldName == "maxmium_queue" && k+1 < len(cmdNode.Content) {
									fmt.Sscanf(cmdNode.Content[k+1].Value, "%d", &cmdTemplate.MaximumQueue)
								} else if fieldName != "" && cmdName == "" {
									cmdName = fieldName
								}
							}
						}
						if cmdName != "" {
							*commands = append(*commands, commandWithLine{
								Name:    cmdName,
								Line:    cmdNode.Line,
								Command: cmdTemplate,
							})
						}
					}
				}
				return
			}
		}

		extractCommandsFromNode(child, commands)
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
