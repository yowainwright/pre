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

var cargoDefaultRegistryRE = regexp.MustCompile(`(?:^|[{,]\s*)default\s*=\s*["']([^"']+)["']`)

type cargoConfigState struct {
	defaultRegistry        string
	offline                string
	resolutionOverride     bool
	resolutionOverridePath string
}

func cargoConfigurationError(args []string) error {
	if cargoHasConfigOverride(args) {
		return errors.New("Cargo --config overrides cannot be scanned")
	}
	if cargoHasFlag(args, "--lockfile-path") {
		return errors.New("Cargo --lockfile-path overrides cannot be scanned")
	}
	if cargoHasFlag(args, "--offline", "--frozen") {
		return errors.New("Cargo offline resolution cannot be scanned")
	}
	if option := cargoUnsupportedUnstableOption(args); option != "" {
		return fmt.Errorf("Cargo unstable option %q cannot be scanned", option)
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
		if arg != "-Z" && !strings.HasPrefix(arg, "-Z") {
			continue
		}
		if arg == "-Z" && index+1 < len(args) {
			index++
			arg = args[index]
		} else {
			arg = strings.TrimPrefix(arg, "-Z")
			arg = strings.TrimPrefix(arg, "=")
		}
		if arg != "" && arg != "unstable-options" {
			return arg
		}
	}
	return ""
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
		if arg == "--config" || strings.HasPrefix(arg, "--config=") {
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
	if err != nil || !includeLocal {
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
	if err != nil || legacyExists {
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
	if (section == "registry" && key == "default") || (section == "" && key == "registry.default") {
		s.defaultRegistry = cargoConfigString(value)
	}
	if section == "" && key == "registry" {
		s.defaultRegistry = cargoInlineDefaultRegistry(value)
	}
	if (section == "net" && key == "offline") || (section == "" && key == "net.offline") {
		s.offline = cargoConfigString(value)
	}
}

func cargoConfigKeyChangesResolution(section, key string) bool {
	if section == "" && (key == "include" || key == "paths") {
		return true
	}
	if section == "" && (key == "patch" || key == "source") {
		return true
	}
	isCratesSource := section == "source.crates-io"
	isDottedSource := section == "" && strings.HasPrefix(key, "source.crates-io.")
	isCratesIndex := section == "registries.crates-io" && key == "index"
	isDottedIndex := section == "" && key == "registries.crates-io.index"
	isLockfileOverride := section == "resolver" && key == "lockfile-path"
	isDottedLockfile := section == "" && key == "resolver.lockfile-path"
	return isCratesSource || isDottedSource || isCratesIndex || isDottedIndex || isLockfileOverride || isDottedLockfile
}

func normalizeCargoConfigKey(value string) string {
	trimmed := strings.TrimSpace(value)
	replacer := strings.NewReplacer(`"`, "", `'`, "")
	return replacer.Replace(trimmed)
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
		return fmt.Errorf("Cargo resolution override in %s cannot be scanned", state.resolutionOverridePath)
	}
	if err := cargoOfflineEnvironmentError(state.offline); err != nil {
		return err
	}
	if err := cargoRegistryIndexEnvironmentError(); err != nil {
		return err
	}
	if command != "add" && command != "install" {
		return nil
	}
	explicitRegistry := cargoFlagValue(args, "--registry")
	if explicitRegistry == "crates-io" {
		return nil
	}
	registry := strings.TrimSpace(os.Getenv("CARGO_REGISTRY_DEFAULT"))
	if registry == "" {
		registry = state.defaultRegistry
	}
	if registry == "" || registry == "crates-io" {
		return nil
	}
	return fmt.Errorf("Cargo default registry %q cannot be scanned as crates.io", registry)
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
		return errors.New("Cargo offline resolution cannot be scanned")
	default:
		return fmt.Errorf("Cargo offline setting %q cannot be scanned", value)
	}
}

func cargoRegistryIndexEnvironmentError() error {
	index := strings.TrimSpace(os.Getenv("CARGO_REGISTRIES_CRATES_IO_INDEX"))
	if index == "" || isCratesIOIndex(index) {
		return nil
	}
	return fmt.Errorf("Cargo crates.io index override %q cannot be scanned", index)
}

func isCratesIOIndex(index string) bool {
	normalized := strings.TrimSuffix(index, "/")
	gitIndex := "https://github.com/rust-lang/crates.io-index"
	sparseIndex := "sparse+https://index.crates.io"
	return normalized == gitIndex || normalized == sparseIndex
}
