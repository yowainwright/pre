package proxy

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/yowainwright/pre/internal/manager"
)

func installPackageArgs(mgr *manager.Manager, args []string) ([]string, bool) {
	missingCommand := mgr == nil || len(args) == 0
	if missingCommand {
		return nil, false
	}
	if mgr.Name == "cargo" {
		return cargoInterceptArgs(mgr, args)
	}
	commandIndex := managerCommandIndex(mgr, args)
	if commandIndex < 0 {
		return nil, false
	}
	return interceptCommandArgs(mgr, args[commandIndex], args[commandIndex+1:])
}

func interceptCommandArgs(mgr *manager.Manager, command string, args []string) ([]string, bool) {
	if slices.Contains(mgr.InstallCmds, command) {
		isGoRemoval := mgr.Name == "go" && command == "get" && goRemovesOnly(mgr, args)
		if isGoRemoval {
			return nil, false
		}
		if isManifestInstall(mgr.Name, command) {
			return nil, true
		}
		return args, true
	}
	isUVPip := mgr.Name == "uv" && command == "pip"
	hasInstallArg := len(args) > 0 && args[0] == "install"
	isUVPipInstall := isUVPip && hasInstallArg
	if isUVPipInstall {
		return args[1:], true
	}
	return nil, false
}

func managerCommandIndex(mgr *manager.Manager, args []string) int {
	for index := 0; index < len(args); index++ {
		known, consumesNext := managerGlobalFlag(mgr, args[index])
		if known {
			if consumesNext {
				index++
			}
			continue
		}
		if !strings.HasPrefix(args[index], "-") {
			return index
		}
	}
	return -1
}

func managerGlobalFlag(mgr *manager.Manager, arg string) (bool, bool) {
	for _, flag := range managerGlobalValueFlags(mgr) {
		isAttachedLong := strings.HasPrefix(arg, flag+"=")
		isAttachedShort := len(flag) == 2 && strings.HasPrefix(arg, flag) && len(arg) > 2
		switch {
		case arg == flag:
			return true, true
		case isAttachedLong || isAttachedShort:
			return true, false
		}
	}
	return false, false
}

func managerGlobalValueFlags(mgr *manager.Manager) []string {
	flags := projectDirectoryFlags(mgr)
	if mgr == nil {
		return flags
	}
	switch mgr.Name {
	case "npm":
		return append(flags, "--workspace", "-w", "--registry", "--userconfig")
	case "pnpm":
		return append(flags, "--filter")
	case "go":
		return []string{"-C"}
	default:
		return flags
	}
}

func goRemovesOnly(mgr *manager.Manager, args []string) bool {
	packages := extractPackages(mgr, args)
	if len(packages) == 0 {
		return false
	}
	for _, spec := range packages {
		if !isGoRemoval(mgr, spec) {
			return false
		}
	}
	return true
}

func isGoRemoval(mgr *manager.Manager, spec string) bool {
	_, version := manager.ParseSpec(mgr.Ecosystem, spec)
	return strings.EqualFold(version, "none")
}

func isManifestInstall(managerName, command string) bool {
	switch managerName {
	case "npm":
		return command == "ci"
	case "uv":
		return command == "sync"
	case "poetry":
		return command == "install"
	default:
		return false
	}
}

func cargoInterceptArgs(mgr *manager.Manager, args []string) ([]string, bool) {
	commandIndex := cargoSubcommandIndex(args)
	if commandIndex < 0 {
		return nil, false
	}
	command := args[commandIndex]
	if !slices.Contains(mgr.InstallCmds, command) {
		return nil, false
	}
	commandArgs := args[commandIndex+1:]
	if cargoIsInformational(command, args) {
		return nil, false
	}
	return cargoCommandPackages(mgr, command, commandArgs), true
}

func cargoCommandPackages(mgr *manager.Manager, command string, args []string) []string {
	switch command {
	case "fetch":
		return nil
	case "update":
		return cargoUpdatePackages(mgr, args)
	case "add":
		return cargoAddPackages(mgr, args)
	case "install":
		return cargoInstallPackages(mgr, args)
	default:
		return nil
	}
}

func cargoSubcommandIndex(args []string) int {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		toolchainSelector := index == 0 && strings.HasPrefix(arg, "+")
		if toolchainSelector {
			continue
		}
		if cargoGlobalFlagConsumesValue(arg) {
			if !strings.Contains(arg, "=") {
				index++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return index
	}
	return -1
}

func cargoGlobalFlagConsumesValue(arg string) bool {
	flag := arg
	if index := strings.Index(flag, "="); index >= 0 {
		flag = flag[:index]
	}
	flags := []string{"--color", "--config", "--explain", "--manifest-path", "--target-dir", "-C", "-Z"}
	return slices.Contains(flags, flag)
}

func cargoIsInformational(command string, args []string) bool {
	showHelp := slices.Contains(args, "--help") || slices.Contains(args, "-h")
	listInstalls := command == "install" && slices.Contains(args, "--list")
	return showHelp || listInstalls
}

func cargoAddPackages(mgr *manager.Manager, args []string) []string {
	if cargoUsesExternalSource(args) {
		return nil
	}
	packages := extractPackages(mgr, args)
	result := make([]string, 0, len(packages))
	for _, spec := range packages {
		result = append(result, normalizeCargoAddSpec(spec))
	}
	return result
}

func normalizeCargoAddSpec(spec string) string {
	name, version := manager.ParseSpec("crates.io", spec)
	if version == "" {
		return name
	}
	startsWithDigit := version[0] >= '0' && version[0] <= '9'
	if startsWithDigit {
		version = "^" + version
	}
	normalized := name + "@" + version
	return normalized
}

func cargoInstallPackages(mgr *manager.Manager, args []string) []string {
	if cargoUsesExternalSource(args) {
		return nil
	}
	packages := extractPackages(mgr, args)
	flagVersion := cargoFlagValue(args, "--version", "--vers")
	result := make([]string, 0, len(packages))
	for _, spec := range packages {
		name, specVersion := manager.ParseSpec("crates.io", spec)
		if specVersion == "" {
			specVersion = flagVersion
		}
		result = append(result, cargoInstallSpec(name, specVersion))
	}
	return result
}

func cargoInstallSpec(name, version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return name
	}
	spec := name + "@" + version
	return spec
}

func cargoUpdatePackages(mgr *manager.Manager, args []string) []string {
	precise := cargoFlagValue(args, "--precise")
	invalidPrecise := precise == "" || !crateExactVersionRE.MatchString(precise)
	if invalidPrecise {
		return nil
	}
	targets := cargoUpdateTargets(mgr, args)
	if len(targets) != 1 {
		return nil
	}
	spec := targets[0] + "@" + precise
	return []string{spec}
}

func cargoFlagValue(args []string, flags ...string) string {
	values := cargoFlagValues(args, flags...)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func cargoFlagValues(args []string, flags ...string) []string {
	known := cargoFlagSet(flags)
	var values []string
	for index, arg := range args {
		flag, value, inline := strings.Cut(arg, "=")
		if !known[flag] {
			continue
		}
		if inline {
			values = append(values, value)
			continue
		}
		if index+1 < len(args) {
			values = append(values, args[index+1])
		}
	}
	return values
}

func cargoUsesExternalSource(args []string) bool {
	if cargoHasFlag(args, "--git", "--path", "--index") {
		return true
	}
	registries := cargoFlagValues(args, "--registry")
	return slices.ContainsFunc(registries, func(registry string) bool {
		return registry != "crates-io"
	})
}

func cargoHasFlag(args []string, flags ...string) bool {
	for _, arg := range args {
		flag := arg
		if index := strings.Index(flag, "="); index >= 0 {
			flag = flag[:index]
		}
		if slices.Contains(flags, flag) {
			return true
		}
	}
	return false
}

func cargoInstallError(mgr *manager.Manager, args []string) error {
	notCargo := mgr == nil || mgr.Name != "cargo"
	if notCargo {
		return nil
	}
	if cargoUsesExternalSource(args) {
		return errors.New("cargo Git, path, and custom-registry sources cannot be scanned")
	}
	if err := cargoConfigurationError(args); err != nil {
		return err
	}
	_, _, err := cargoManifestPath(args)
	return err
}

func npmInstallError(mgr *manager.Manager, args []string) error {
	notNPM := mgr == nil || mgr.Ecosystem != "npm"
	if notNPM {
		return nil
	}
	if err := npmSourceFlagError(mgr, args); err != nil {
		return err
	}
	for _, arg := range npmPackageArguments(mgr, args) {
		if unsupportedNPMPackageSource(arg) {
			return fmt.Errorf("%s dependency source %q cannot be scanned", mgr.Name, arg)
		}
	}
	return nil
}

func npmSourceFlagError(mgr *manager.Manager, args []string) error {
	for index := 0; index < len(args); index++ {
		flag, value, found := npmSourceFlagAt(args, index)
		if !found {
			continue
		}
		if !strings.Contains(args[index], "=") {
			index++
		}
		if flag == "--userconfig" {
			return fmt.Errorf("%s userconfig %q cannot be scanned", mgr.Name, value)
		}
		if !isPublicNPMRegistry(value) {
			return fmt.Errorf("%s registry %q cannot be scanned", mgr.Name, value)
		}
	}
	return nil
}

func npmSourceFlagAt(args []string, index int) (string, string, bool) {
	for _, flag := range []string{"--registry", "--userconfig"} {
		if value, ok := strings.CutPrefix(args[index], flag+"="); ok {
			return flag, value, true
		}
		hasSeparateValue := args[index] == flag && index+1 < len(args)
		if hasSeparateValue {
			return flag, args[index+1], true
		}
		if args[index] == flag {
			return flag, "", true
		}
	}
	return "", "", false
}

func isPublicNPMRegistry(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	hasHTTPS := parsed.Scheme == "https"
	hasAnonymousURL := parsed.User == nil
	hasRegistryHost := strings.EqualFold(parsed.Hostname(), "registry.npmjs.org")
	hasDefaultPort := parsed.Port() == ""
	hasRegistryURL := hasHTTPS && hasAnonymousURL && hasRegistryHost
	return hasRegistryURL && hasDefaultPort
}

func npmPackageArguments(mgr *manager.Manager, args []string) []string {
	var packages []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if packageFlagConsumesValue(mgr, arg) {
			if !strings.Contains(arg, "=") {
				index++
			}
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			packages = append(packages, arg)
		}
	}
	return packages
}

func unsupportedNPMPackageSource(spec string) bool {
	if strings.Contains(spec, "@npm:") {
		return true
	}
	requested := npmRequestedSpec(spec)
	unsupported := requested != "" && !manager.IsSupportedNPMRegistrySpec(requested)
	return unsupported
}

func npmRequestedSpec(spec string) string {
	if strings.HasPrefix(spec, "@") {
		if index := strings.LastIndex(spec, "@"); index > 0 {
			return spec[index+1:]
		}
		return ""
	}
	name, requested, found := strings.Cut(spec, "@")
	hasRequestedVersion := found && name != ""
	if hasRequestedVersion {
		return requested
	}
	return spec
}

func cargoManifestPath(args []string) (string, bool, error) {
	path, explicit, err := findCargoManifestPath(args)
	if err != nil {
		return "", explicit, err
	}
	workingDir, changedDir, err := cargoWorkingDirectory(args)
	if err != nil {
		return "", changedDir, err
	}
	relativeToWorkingDir := changedDir && !filepath.IsAbs(path)
	if relativeToWorkingDir {
		path = filepath.Join(workingDir, path)
	}
	return path, explicit, nil
}

func findCargoManifestPath(args []string) (string, bool, error) {
	for index, arg := range args {
		value, inline := strings.CutPrefix(arg, "--manifest-path=")
		if inline {
			return cargoInlineManifestPath(value)
		}
		if arg == "--manifest-path" {
			return cargoSeparateManifestPath(args, index)
		}
	}
	return "Cargo.toml", false, nil
}

func cargoWorkingDirectory(args []string) (string, bool, error) {
	for index, arg := range args {
		if arg == "-C" {
			if index+1 >= len(args) {
				return "", true, errors.New("-C requires a value")
			}
			return args[index+1], true, nil
		}
		value, ok := strings.CutPrefix(arg, "-C")
		hasInlineDirectory := ok && value != ""
		if hasInlineDirectory {
			value = strings.TrimPrefix(value, "=")
			if value == "" {
				return "", true, errors.New("-C requires a value")
			}
			return value, true, nil
		}
	}
	return "", false, nil
}

func cargoInlineManifestPath(value string) (string, bool, error) {
	if value == "" {
		return "", true, errors.New("--manifest-path requires a value")
	}
	return value, true, nil
}

func cargoSeparateManifestPath(args []string, index int) (string, bool, error) {
	missing := index+1 >= len(args)
	if !missing {
		missing = strings.HasPrefix(args[index+1], "-")
	}
	if missing {
		return "", true, errors.New("--manifest-path requires a value")
	}
	return args[index+1], true, nil
}

func installFallbackPackages(mgr *manager.Manager, args []string) ([]string, error) {
	isCargo := mgr != nil && mgr.Name == "cargo"
	if isCargo {
		return cargoFallbackPackages(mgr, args)
	}
	dir, err := installProjectDir(mgr, args)
	if err != nil {
		return nil, err
	}
	return readProjectPackages(mgr, dir, args)
}

func readProjectPackages(mgr *manager.Manager, dir string, args []string) ([]string, error) {
	isNPM := false
	if mgr != nil {
		isNPM = mgr.Name == "npm" && mgr.Ecosystem == "npm"
	}
	if isNPM {
		packages, _, err := manager.ReadNPMProject(dir, args...)
		return packages, err
	}
	if err := validateManifestFn(mgr, dir, args...); err != nil {
		return nil, err
	}
	return readManifestDirFn(mgr, dir), nil
}

func installProjectDir(mgr *manager.Manager, args []string) (string, error) {
	flags := projectDirectoryFlags(mgr)
	values, err := projectDirectoryValues(args, flags)
	directoryUnavailable := err != nil || len(values) == 0
	if directoryUnavailable {
		return ".", err
	}
	for _, value := range values[1:] {
		if value != values[0] {
			return "", errors.New("conflicting project directory flags")
		}
	}
	return filepath.Clean(values[0]), nil
}

func projectDirectoryFlags(mgr *manager.Manager) []string {
	if mgr == nil {
		return nil
	}
	switch mgr.Name {
	case "npm":
		return []string{"--prefix"}
	case "bun":
		return []string{"--cwd"}
	case "pnpm":
		return []string{"--dir", "-C"}
	case "uv":
		return []string{"--project"}
	case "poetry":
		return []string{"--project", "-P"}
	default:
		return nil
	}
}

func projectDirectoryValues(args, flags []string) ([]string, error) {
	var values []string
	for index := 0; index < len(args); index++ {
		value, consumed, err := projectDirectoryValueAt(args, index, flags)
		if err != nil {
			return nil, err
		}
		if value != "" {
			values = append(values, value)
		}
		if consumed {
			index++
		}
	}
	return values, nil
}

func projectDirectoryValueAt(args []string, index int, flags []string) (string, bool, error) {
	for _, flag := range flags {
		if value, ok := strings.CutPrefix(args[index], flag+"="); ok {
			const consumedNext = false
			return requireProjectDirectory(value, consumedNext, flag)
		}
		if args[index] == flag {
			missingValue := index+1 >= len(args) || strings.HasPrefix(args[index+1], "-")
			if missingValue {
				return "", false, fmt.Errorf("%s requires a value", flag)
			}
			const consumedNext = true
			return requireProjectDirectory(args[index+1], consumedNext, flag)
		}
	}
	return "", false, nil
}

func requireProjectDirectory(value string, consumed bool, flag string) (string, bool, error) {
	if value == "" {
		return "", consumed, fmt.Errorf("%s requires a value", flag)
	}
	return value, consumed, nil
}

func cargoFallbackPackages(mgr *manager.Manager, args []string) ([]string, error) {
	path, explicit, err := cargoProjectManifest(args)
	missingManifest := err != nil && errors.Is(err, os.ErrNotExist)
	if missingManifest {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return cargoFallbackFromManifest(mgr, args, path, explicit)
}

func cargoFallbackFromManifest(mgr *manager.Manager, args []string, path string, explicit bool) ([]string, error) {
	commandIndex := cargoSubcommandIndex(args)
	if commandIndex < 0 {
		return nil, errors.New("cargo subcommand is missing")
	}
	commandArgs := args[commandIndex+1:]
	packages, err := readCargoFallback(mgr, args[commandIndex], commandArgs, path)
	missingImplicitManifest := err != nil && !explicit && errors.Is(err, os.ErrNotExist)
	if missingImplicitManifest {
		return nil, nil
	}
	return packages, err
}

func validateCargoDirectPackages(mgr *manager.Manager, args, packages []string) error {
	notCargo := mgr == nil || mgr.Name != "cargo"
	skipValidation := notCargo || len(packages) == 0
	if skipValidation {
		return nil
	}
	commandIndex := cargoSubcommandIndex(args)
	projectCommands := []string{"add", "update"}
	notProjectCommand := commandIndex < 0 || !slices.Contains(projectCommands, args[commandIndex])
	if notProjectCommand {
		return nil
	}
	return validateCargoProjectArgs(args)
}

func validateCargoProjectArgs(args []string) error {
	path, explicit, err := cargoProjectManifest(args)
	missingImplicitManifest := err != nil && !explicit && errors.Is(err, os.ErrNotExist)
	if missingImplicitManifest {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = readCargoUpdatePackagesFn(path, "")
	return err
}

func cargoProjectManifest(args []string) (string, bool, error) {
	path, explicit, err := cargoManifestPath(args)
	manifestResolved := err != nil || explicit
	if manifestResolved {
		return path, explicit, err
	}
	path, err = manager.DiscoverCargoManifest(path)
	return path, false, err
}

func readCargoFallback(mgr *manager.Manager, command string, args []string, path string) ([]string, error) {
	switch command {
	case "fetch", "install":
		return readCargoFetchPackagesFn(path)
	case "update":
		return readCargoUpdateFallback(mgr, args, path)
	default:
		return nil, nil
	}
}

func readCargoUpdateFallback(mgr *manager.Manager, args []string, path string) ([]string, error) {
	targets := cargoUpdateTargets(mgr, args)
	if len(targets) == 0 {
		return readCargoUpdatePackagesFn(path, "")
	}
	var packages []string
	for _, target := range targets {
		selected, err := readCargoUpdatePackagesFn(path, target)
		if err != nil {
			return nil, err
		}
		packages = append(packages, selected...)
	}
	return uniquePackages(packages), nil
}

func cargoUpdateTargets(mgr *manager.Manager, args []string) []string {
	packageSpecs := extractPackages(mgr, args)
	packageSpecs = append(packageSpecs, cargoFlagValues(args, "-p", "--package")...)
	var targets []string
	for _, spec := range packageSpecs {
		name, _ := manager.ParseSpec("crates.io", spec)
		if manager.IsValidCrateName(name) {
			targets = append(targets, name)
		}
	}
	return uniquePackages(targets)
}

func installPackages(mgr *manager.Manager, args []string) ([]string, error) {
	if err := pythonInstallError(mgr, args); err != nil {
		return nil, err
	}
	packages := extractPackages(mgr, args)
	packages = withoutGoRemovals(mgr, packages)
	for _, path := range requirementFilePaths(mgr, args) {
		fromFile, err := readRequirementsFileFn(path)
		if err != nil {
			return nil, fmt.Errorf("read requirements %q: %w", path, err)
		}
		packages = append(packages, fromFile...)
	}
	return uniquePackages(packages), nil
}

func pythonInstallError(mgr *manager.Manager, args []string) error {
	notPython := mgr == nil || mgr.Ecosystem != "PyPI"
	if notPython {
		return nil
	}
	return walkPackageArgs(mgr, args, func(index int, option bool) error {
		arg := args[index]
		shortEditable := strings.HasPrefix(arg, "-e")
		longEditable := arg == "--editable" || strings.HasPrefix(arg, "--editable=")
		editableFlag := shortEditable || longEditable
		editable := option && editableFlag
		unsupportedTarget := !option && !isPackageArg(mgr, arg)
		unsupportedSource := editable || unsupportedTarget
		if unsupportedSource {
			return errors.New("unsupported python dependency source cannot be scanned")
		}
		return nil
	})
}

func withoutGoRemovals(mgr *manager.Manager, packages []string) []string {
	notGo := mgr == nil || mgr.Ecosystem != "Go"
	if notGo {
		return packages
	}
	result := make([]string, 0, len(packages))
	for _, spec := range packages {
		if !isGoRemoval(mgr, spec) {
			result = append(result, spec)
		}
	}
	return result
}

func requirementFilePaths(mgr *manager.Manager, args []string) []string {
	notPython := mgr == nil || mgr.Ecosystem != "PyPI"
	if notPython {
		return nil
	}
	return pythonRequirementPaths(args)
}

func pythonRequirementPaths(args []string) []string {
	var paths []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		path, consumeNext := requirementPathAt(args, index)
		if path != "" {
			paths = append(paths, path)
		}
		if consumeNext {
			index++
		}
	}
	return paths
}

func requirementPathAt(args []string, index int) (string, bool) {
	arg := args[index]
	flags := []string{"-r", "--requirement", "--requirements"}
	hasSeparatePath := slices.Contains(flags, arg) && index+1 < len(args)
	if hasSeparatePath {
		return args[index+1], true
	}
	return inlineRequirementPath(arg), false
}

func inlineRequirementPath(arg string) string {
	for _, prefix := range []string{"--requirement=", "--requirements=", "-r"} {
		path, ok := strings.CutPrefix(arg, prefix)
		hasInlinePath := ok && path != ""
		if hasInlinePath {
			return path
		}
	}
	return ""
}

func uniquePackages(packages []string) []string {
	seen := make(map[string]bool, len(packages))
	result := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		result = append(result, pkg)
	}
	return result
}

func extractPackages(mgr *manager.Manager, args []string) []string {
	result := make([]string, 0, len(args))
	_ = walkPackageArgs(mgr, args, func(index int, option bool) error {
		arg := args[index]
		packageArg := !option && isPackageArg(mgr, arg)
		if packageArg {
			result = append(result, arg)
		}
		return nil
	})
	return result
}

func walkPackageArgs(mgr *manager.Manager, args []string, visit func(int, bool) error) error {
	afterTerminator := false
	for index := 0; index < len(args); index++ {
		if args[index] == "--" {
			afterTerminator = true
			continue
		}
		option, consumeNext := packageOption(mgr, args[index], afterTerminator)
		if err := visit(index, option); err != nil {
			return err
		}
		if consumeNext {
			index++
		}
	}
	return nil
}

func packageOption(mgr *manager.Manager, arg string, afterTerminator bool) (bool, bool) {
	if afterTerminator {
		return false, false
	}
	if packageFlagConsumesValue(mgr, arg) {
		return true, !strings.Contains(arg, "=")
	}
	return strings.HasPrefix(arg, "-"), false
}

func packageFlagConsumesValue(mgr *manager.Manager, arg string) bool {
	if mgr == nil {
		return false
	}
	flag := arg
	if idx := strings.Index(flag, "="); idx != -1 {
		flag = flag[:idx]
	}

	return ecosystemFlagConsumesValue(mgr, flag)
}

func ecosystemFlagConsumesValue(mgr *manager.Manager, flag string) bool {
	switch mgr.Ecosystem {
	case "npm":
		return npmFlagConsumesValue(flag)
	case "PyPI":
		return pythonFlagConsumesValue(mgr.Name, flag)
	case "Go":
		return goFlagConsumesValue(flag)
	case "Homebrew":
		consumesValue := flag == "--appdir" || flag == "--cc"
		return consumesValue
	case "crates.io":
		return cargoFlagConsumesValue(flag)
	}

	return false
}

func npmFlagConsumesValue(flag string) bool {
	flags := []string{
		"--workspace", "-w", "--prefix", "--cwd", "--dir", "-C", "--tag", "--registry", "--userconfig", "--cache",
		"--omit", "--include", "--install-strategy", "--save-prefix", "--otp", "--before", "--scope",
	}
	return slices.Contains(flags, flag)
}

func pythonFlagConsumesValue(managerName, flag string) bool {
	flags := []string{
		"-r", "--requirement", "--requirements", "-c", "--constraint", "-i", "--index-url", "--index", "--default-index",
		"--extra-index-url", "-f", "--find-links", "--trusted-host", "--python", "--platform", "--python-version",
		"--implementation", "--abi", "-t", "--target", "--root", "--prefix", "--src", "--upgrade-strategy",
		"--config-settings", "-C", "--global-option", "--build-option", "--only-binary", "--no-binary", "--report", "-e", "--editable",
		"--project", "-P",
	}
	if managerName == "uv" {
		flags = append(flags, "-p", "--constraints", "--config-setting")
	}
	if slices.Contains(flags, flag) {
		return true
	}
	usesGroups := managerName == "poetry" || managerName == "uv"
	groupFlags := []string{"--group", "-G", "--source", "--extras"}
	return usesGroups && slices.Contains(groupFlags, flag)
}

func goFlagConsumesValue(flag string) bool {
	flags := []string{
		"-C", "-mod", "-modfile", "-overlay", "-pgo", "-asmflags",
		"-gcflags", "-ldflags", "-tags", "-toolexec", "-pkgdir",
	}
	return slices.Contains(flags, flag)
}

func cargoFlagConsumesValue(flag string) bool {
	flags := []string{
		"--version", "--vers", "--git", "--branch", "--tag", "--rev", "--path", "--base",
		"--registry", "--index", "--target", "--rename", "-F", "--features", "-p", "--package",
		"--manifest-path", "--root", "--bin", "--example", "--profile", "--target-dir", "-j", "--jobs",
		"--color", "--message-format", "--config", "-C", "-Z", "--precise", "--exclude", "--lockfile-path",
	}
	return slices.Contains(flags, flag)
}

func isPackageArg(mgr *manager.Manager, arg string) bool {
	unsupportedPrefix := arg == "" || hasUnsupportedPackagePrefix(arg)
	if unsupportedPrefix {
		return false
	}
	if mgr == nil {
		return true
	}
	switch mgr.Ecosystem {
	case "npm":
		return !strings.Contains(arg, "@npm:")
	case "PyPI":
		return !isPythonPackageFile(arg)
	case "crates.io":
		name, _ := manager.ParseSpec(mgr.Ecosystem, arg)
		return manager.IsValidCrateName(name)
	default:
		return true
	}
}

func hasUnsupportedPackagePrefix(arg string) bool {
	prefixes := []string{
		"-", ".", "/", "~/", "file:", "link:", "git+", "git://",
		"ssh://", "git@", "github:", "http://", "https://",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

func isPythonPackageFile(arg string) bool {
	lower := strings.ToLower(arg)
	suffixes := []string{".txt", ".whl", ".zip", ".egg", ".tar.gz", ".tgz"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func shouldResolveVersion(ecosystem, version string) bool {
	if version == "" {
		return true
	}

	switch ecosystem {
	case "npm":
		switch strings.ToLower(version) {
		case "*", "alpha", "beta", "canary", "head", "latest", "main", "master", "next", "stable", "tip":
			return true
		}
	case "Go":
		return strings.ToLower(version) == "latest"
	}

	return false
}

func cargoFlagSet(flags []string) map[string]bool {
	known := make(map[string]bool, len(flags))
	for _, flag := range flags {
		known[flag] = true
	}
	return known
}
