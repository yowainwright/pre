package proxy

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var (
	cargoDefaultRegistryRE = regexp.MustCompile(`(?:^|[{,]\s*)default\s*=\s*["']([^"']+)["']`)
	cargoConfigKeyReplacer = strings.NewReplacer(`"`, "", `'`, "")
)

type cargoConfigState struct {
	defaultRegistry        string
	offline                string
	resolutionOverride     bool
	resolutionOverridePath string
}

func cargoConfigurationError(args []string) error {
	if cargoHasConfigOverride(args) {
		return errors.New("cargo --config overrides cannot be scanned")
	}
	if cargoHasFlag(args, "--lockfile-path") {
		return errors.New("cargo --lockfile-path overrides cannot be scanned")
	}
	if cargoHasFlag(args, "--offline", "--frozen") {
		return errors.New("cargo offline resolution cannot be scanned")
	}
	if option := cargoUnsupportedUnstableOption(args); option != "" {
		return fmt.Errorf("cargo unstable option %q cannot be scanned", option)
	}
	command := cargoCommand(args)
	state, err := loadCargoConfigState(args, command)
	if err != nil {
		return err
	}
	return validateCargoConfigState(args, command, state)
}

func cargoUnsupportedUnstableOption(args []string) string {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "-Z") {
			continue
		}
		var consumed bool
		arg, consumed = cargoUnstableOption(args, index)
		if consumed {
			index++
		}
		unsupported := arg != "" && arg != "unstable-options"
		if unsupported {
			return arg
		}
	}
	return ""
}

func cargoUnstableOption(args []string, index int) (string, bool) {
	separate := args[index] == "-Z" && index+1 < len(args)
	if separate {
		return args[index+1], true
	}
	option := strings.TrimPrefix(args[index], "-Z")
	return strings.TrimPrefix(option, "="), false
}

func loadCargoConfigState(args []string, command string) (cargoConfigState, error) {
	startDir, _, err := cargoWorkingDirectory(args)
	if err != nil {
		return cargoConfigState{}, err
	}
	paths, err := cargoConfigPaths(startDir, command != "install")
	if err != nil {
		return cargoConfigState{}, err
	}
	return readCargoConfigState(paths)
}

func cargoHasConfigOverride(args []string) bool {
	for _, arg := range args {
		configOverride := arg == "--config" || strings.HasPrefix(arg, "--config=")
		if configOverride {
			return true
		}
	}
	return false
}

func cargoCommand(args []string) string {
	index := cargoSubcommandIndex(args)
	if index < 0 {
		return ""
	}
	return args[index]
}

func cargoConfigPaths(startDir string, includeLocal bool) ([]string, error) {
	home, err := cargoHomePath()
	if err != nil {
		return nil, err
	}
	paths, err := appendCargoConfig(nil, home)
	skipLocal := err != nil || !includeLocal
	if skipLocal {
		return paths, err
	}
	return appendLocalCargoConfigs(paths, startDir)
}

func appendLocalCargoConfigs(paths []string, startDir string) ([]string, error) {
	localDirs, err := cargoLocalConfigDirs(startDir)
	if err != nil {
		return nil, err
	}
	for _, dir := range localDirs {
		paths, err = appendCargoConfig(paths, filepath.Join(dir, ".cargo"))
		if err != nil {
			return nil, err
		}
	}
	return uniquePackages(paths), nil
}

func cargoHomePath() (string, error) {
	if cargoHome := strings.TrimSpace(os.Getenv("CARGO_HOME")); cargoHome != "" {
		return filepath.Abs(cargoHome)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find Cargo home: %w", err)
	}
	return filepath.Join(home, ".cargo"), nil
}

func cargoLocalConfigDirs(startDir string) ([]string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for {
		dirs = append(dirs, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			slices.Reverse(dirs)
			return dirs, nil
		}
		dir = parent
	}
}

func appendCargoConfig(paths []string, configDir string) ([]string, error) {
	legacyPath := filepath.Join(configDir, "config")
	legacyExists, err := cargoConfigExists(legacyPath)
	legacyResolved := err != nil || legacyExists
	if legacyResolved {
		return appendIfCargoConfig(paths, legacyPath, legacyExists), err
	}
	tomlPath := filepath.Join(configDir, "config.toml")
	tomlExists, err := cargoConfigExists(tomlPath)
	return appendIfCargoConfig(paths, tomlPath, tomlExists), err
}

func cargoConfigExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func appendIfCargoConfig(paths []string, path string, exists bool) []string {
	if !exists {
		return paths
	}
	return append(paths, path)
}

func readCargoConfigState(paths []string) (cargoConfigState, error) {
	var merged cargoConfigState
	for _, path := range paths {
		state, err := readCargoConfig(path)
		if err != nil {
			return cargoConfigState{}, err
		}
		if state.defaultRegistry != "" {
			merged.defaultRegistry = state.defaultRegistry
		}
		if state.offline != "" {
			merged.offline = state.offline
		}
		if state.resolutionOverride {
			merged.resolutionOverride = true
			merged.resolutionOverridePath = path
		}
	}
	return merged, nil
}

func readCargoConfig(path string) (cargoConfigState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cargoConfigState{}, fmt.Errorf("read Cargo config %s: %w", path, err)
	}
	state, err := parseCargoConfig(data)
	if err != nil {
		return cargoConfigState{}, fmt.Errorf("parse Cargo config %s: %w", path, err)
	}
	return state, nil
}

func parseCargoConfig(data []byte) (cargoConfigState, error) {
	var state cargoConfigState
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := normalizeCargoConfigLine(scanner.Text())
		if next, ok := cargoConfigSection(line); ok {
			section = next
			state.resolutionOverride = state.resolutionOverride || cargoConfigSectionChangesResolution(section)
			continue
		}
		state.consume(section, line)
	}
	return state, scanner.Err()
}

func normalizeCargoConfigLine(line string) string {
	line, _, _ = strings.Cut(line, "#")
	return strings.TrimSpace(line)
}

func cargoConfigSection(line string) (string, bool) {
	hasBrackets := strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
	if !hasBrackets {
		return "", false
	}
	section := strings.TrimSpace(strings.Trim(line, "[]"))
	return normalizeCargoConfigKey(section), true
}

func cargoConfigSectionChangesResolution(section string) bool {
	isPatch := section == "patch" || strings.HasPrefix(section, "patch.")
	return isPatch
}

func (s *cargoConfigState) consume(section, line string) {
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return
	}
	key = normalizeCargoConfigKey(key)
	if cargoConfigKeyChangesResolution(section, key) {
		s.resolutionOverride = true
	}
	if cargoConfigField(section, key, "registry", "default") {
		s.defaultRegistry = cargoConfigString(value)
	}
	inlineRegistry := section == "" && key == "registry"
	if inlineRegistry {
		s.defaultRegistry = cargoInlineDefaultRegistry(value)
	}
	if cargoConfigField(section, key, "net", "offline") {
		s.offline = cargoConfigString(value)
	}
}

func cargoConfigKeyChangesResolution(section, key string) bool {
	rootOverride := section == "" && slices.Contains([]string{"include", "paths", "patch", "source"}, key)
	if rootOverride {
		return true
	}
	isCratesSource := section == "source.crates-io"
	isDottedSource := section == "" && strings.HasPrefix(key, "source.crates-io.")
	if isCratesSource {
		return true
	}
	if isDottedSource {
		return true
	}
	if cargoConfigField(section, key, "registries.crates-io", "index") {
		return true
	}
	return cargoConfigField(section, key, "resolver", "lockfile-path")
}

func cargoConfigField(section, key, group, field string) bool {
	nested := section == group && key == field
	dotted := section == "" && key == group+"."+field
	return nested || dotted
}

func normalizeCargoConfigKey(value string) string {
	trimmed := strings.TrimSpace(value)
	return cargoConfigKeyReplacer.Replace(trimmed)
}

func cargoInlineDefaultRegistry(value string) string {
	match := cargoDefaultRegistryRE.FindStringSubmatch(value)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func cargoConfigString(value string) string {
	trimmed := strings.TrimSpace(value)
	return strings.Trim(trimmed, `"'`)
}

func validateCargoConfigState(args []string, command string, state cargoConfigState) error {
	if state.resolutionOverride {
		return fmt.Errorf("cargo resolution override in %s cannot be scanned", state.resolutionOverridePath)
	}
	if err := cargoOfflineEnvironmentError(state.offline); err != nil {
		return err
	}
	if err := cargoRegistryIndexEnvironmentError(); err != nil {
		return err
	}
	ignoresDefaultRegistry := command != "add" && command != "install"
	if ignoresDefaultRegistry {
		return nil
	}
	return cargoDefaultRegistryError(args, state.defaultRegistry)
}

func cargoDefaultRegistryError(args []string, defaultRegistry string) error {
	explicitRegistry := cargoFlagValue(args, "--registry")
	if explicitRegistry == "crates-io" {
		return nil
	}
	registry := strings.TrimSpace(os.Getenv("CARGO_REGISTRY_DEFAULT"))
	if registry == "" {
		registry = defaultRegistry
	}
	publicRegistry := registry == "" || registry == "crates-io"
	if publicRegistry {
		return nil
	}
	return fmt.Errorf("cargo default registry %q cannot be scanned as crates.io", registry)
}

func cargoOfflineEnvironmentError(configValue string) error {
	value := configValue
	if environmentValue, exists := os.LookupEnv("CARGO_NET_OFFLINE"); exists {
		value = environmentValue
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false":
		return nil
	case "1", "true":
		return errors.New("cargo offline resolution cannot be scanned")
	default:
		return fmt.Errorf("cargo offline setting %q cannot be scanned", value)
	}
}

func cargoRegistryIndexEnvironmentError() error {
	index := strings.TrimSpace(os.Getenv("CARGO_REGISTRIES_CRATES_IO_INDEX"))
	publicIndex := index == "" || isCratesIOIndex(index)
	if publicIndex {
		return nil
	}
	return fmt.Errorf("cargo crates.io index override %q cannot be scanned", index)
}

func isCratesIOIndex(index string) bool {
	normalized := strings.TrimSuffix(index, "/")
	gitIndex := "https://github.com/rust-lang/crates.io-index"
	sparseIndex := "sparse+https://index.crates.io"
	publicIndex := normalized == gitIndex || normalized == sparseIndex
	return publicIndex
}
