package proxy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/yowainwright/pre/internal/manager"
)

func installPackageArgs(mgr *manager.Manager, args []string) ([]string, bool) {
	if mgr == nil || len(args) == 0 {
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
	isUVPipInstall := mgr.Name == "uv" && command == "pip" && len(args) > 0 && args[0] == "install"
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
		return append(flags, "--workspace", "-w")
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
		if index == 0 && strings.HasPrefix(arg, "+") {
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
	return name + "@" + version
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
	return name + "@" + version
}

func cargoUpdatePackages(mgr *manager.Manager, args []string) []string {
	precise := cargoFlagValue(args, "--precise")
	if precise == "" || !crateExactVersionRE.MatchString(precise) {
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
	var values []string
	for index, arg := range args {
		for _, flag := range flags {
			prefix := flag + "="
			if value, ok := strings.CutPrefix(arg, prefix); ok {
				values = append(values, value)
				break
			}
			if arg == flag && index+1 < len(args) {
				values = append(values, args[index+1])
				break
			}
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
	if mgr == nil || mgr.Name != "cargo" {
		return nil
	}
	if cargoUsesExternalSource(args) {
		return errors.New("Cargo Git, path, and custom-registry sources cannot be scanned")
	}
	if err := cargoConfigurationError(args); err != nil {
		return err
	}
	_, _, err := cargoManifestPath(args)
	return err
}

func npmInstallError(mgr *manager.Manager, args []string) error {
	if mgr == nil || mgr.Ecosystem != "npm" {
		return nil
	}
	for _, arg := range npmPackageArguments(mgr, args) {
		if unsupportedNPMPackageSource(arg) {
			return fmt.Errorf("%s dependency source %q cannot be scanned", mgr.Name, arg)
		}
	}
	return nil
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
	return requested != "" && !manager.IsSupportedNPMRegistrySpec(requested)
}

func npmRequestedSpec(spec string) string {
	if strings.HasPrefix(spec, "@") {
		if index := strings.LastIndex(spec, "@"); index > 0 {
			return spec[index+1:]
		}
		return ""
	}
	if name, requested, found := strings.Cut(spec, "@"); found && name != "" {
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
	if changedDir && !filepath.IsAbs(path) {
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
		if value, ok := strings.CutPrefix(arg, "-C"); ok && value != "" {
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
	if mgr == nil || mgr.Name != "cargo" {
		dir, err := installProjectDir(mgr, args)
		if err != nil {
			return nil, err
		}
		if err := validateManifestFn(mgr, dir); err != nil {
			return nil, err
		}
		return readManifestDirFn(mgr, dir), nil
	}
	return cargoFallbackPackages(mgr, args)
}

func installProjectDir(mgr *manager.Manager, args []string) (string, error) {
	flags := projectDirectoryFlags(mgr)
	values, err := projectDirectoryValues(args, flags)
	if err != nil || len(values) == 0 {
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
			return requireProjectDirectory(value, false, flag)
		}
		if args[index] == flag {
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
				return "", false, fmt.Errorf("%s requires a value", flag)
			}
			return requireProjectDirectory(args[index+1], true, flag)
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
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	commandIndex := cargoSubcommandIndex(args)
	if commandIndex < 0 {
		return nil, errors.New("Cargo subcommand is missing")
	}
	commandArgs := args[commandIndex+1:]
	packages, err := readCargoFallback(mgr, args[commandIndex], commandArgs, path)
	if err != nil && !explicit && errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return packages, err
}

func validateCargoDirectPackages(mgr *manager.Manager, args, packages []string) error {
	if mgr == nil || mgr.Name != "cargo" || len(packages) == 0 {
		return nil
	}
	commandIndex := cargoSubcommandIndex(args)
	projectCommands := []string{"add", "update"}
	if commandIndex < 0 || !slices.Contains(projectCommands, args[commandIndex]) {
		return nil
	}
	path, explicit, err := cargoProjectManifest(args)
	if err != nil && !explicit && errors.Is(err, os.ErrNotExist) {
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
	if err != nil || explicit {
		return path, explicit, err
	}
	path, err = discoverCargoManifest(path)
	return path, false, err
}

func discoverCargoManifest(startPath string) (string, error) {
	return manager.DiscoverCargoManifest(startPath)
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

func withoutGoRemovals(mgr *manager.Manager, packages []string) []string {
	if mgr == nil || mgr.Ecosystem != "Go" {
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
	if mgr == nil || mgr.Ecosystem != "PyPI" {
		return nil
	}
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
	if slices.Contains(flags, arg) && index+1 < len(args) {
		return args[index+1], true
	}
	return inlineRequirementPath(arg), false
}

func inlineRequirementPath(arg string) string {
	for _, prefix := range []string{"--requirement=", "--requirements=", "-r"} {
		if path, ok := strings.CutPrefix(arg, prefix); ok && path != "" {
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
	skipNext := false
	afterTerminator := false

	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			afterTerminator = true
			continue
		}
		if !afterTerminator && packageFlagConsumesValue(mgr, arg) {
			if !strings.Contains(arg, "=") {
				skipNext = true
			}
			continue
		}
		if !afterTerminator && strings.HasPrefix(arg, "-") {
			continue
		}
		if isPackageArg(mgr, arg) {
			result = append(result, arg)
		}
	}

	return result
}

func packageFlagConsumesValue(mgr *manager.Manager, arg string) bool {
	if mgr == nil {
		return false
	}
	flag := arg
	if idx := strings.Index(flag, "="); idx != -1 {
		flag = flag[:idx]
	}

	switch mgr.Ecosystem {
	case "npm":
		return npmFlagConsumesValue(flag)
	case "PyPI":
		return pythonFlagConsumesValue(mgr.Name, flag)
	case "Go":
		return goFlagConsumesValue(flag)
	case "Homebrew":
		return flag == "--appdir" || flag == "--cc"
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
		"--implementation", "--abi", "--target", "--root", "--prefix", "--src", "--upgrade-strategy",
		"--config-settings", "-C", "--global-option", "--build-option", "--only-binary", "--no-binary", "--report", "-e", "--editable",
		"--project", "-P",
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
	if arg == "" || hasUnsupportedPackagePrefix(arg) {
		return false
	}
	if mgr != nil && mgr.Ecosystem == "npm" && strings.Contains(arg, "@npm:") {
		return false
	}
	if mgr != nil && mgr.Ecosystem == "PyPI" && isPythonPackageFile(arg) {
		return false
	}
	if mgr != nil && mgr.Ecosystem == "crates.io" {
		name, _ := manager.ParseSpec(mgr.Ecosystem, arg)
		return manager.IsValidCrateName(name)
	}

	return true
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
