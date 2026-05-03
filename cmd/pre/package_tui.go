package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yowainwright/pre/internal/manager"
)

type packageAction string

const (
	actionInstall   packageAction = "install"
	actionUpdate    packageAction = "update"
	actionUninstall packageAction = "uninstall"
	actionDowngrade packageAction = "downgrade"
)

type installedPackage struct {
	Manager   string
	Ecosystem string
	Name      string
	Version   string
}

type packageInventory struct {
	Packages []installedPackage
	Errors   []string
}

var (
	commandOutputFn               = runCommandOutput
	packageInputReader  io.Reader = os.Stdin
	terminalSizeFn                = detectTerminalSize
	homebrewPrefixesFn            = defaultHomebrewPrefixes
	manageActionPauseFn           = func() { time.Sleep(600 * time.Millisecond) }
)

const (
	keyUp = iota + 1000
	keyDown
	keyEnter
	keyEsc
	keyCtrlC
	keyBackspace
)

const (
	ansiClear      = "\033[2J\033[H"
	ansiAltScreen  = "\033[?1049h"
	ansiMainScreen = "\033[?1049l"
	ansiHideCursor = "\033[?25l"
	ansiShowCursor = "\033[?25h"
	ansiReset      = "\033[0m"
)

const loadingManageMessage = "loading package inventory..."

type manageTheme struct {
	title        string
	subtitle     string
	help         string
	muted        string
	tableHeader  string
	search       string
	searchActive string
	selected     string
	warning      string
	dialog       string
	dialogTitle  string
	dialogHelp   string
	footer       string
}

func currentManageTheme() manageTheme {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PRE_MANAGE_THEME"))) {
	case "", "catppuccin", "mocha", "catppuccin-mocha":
		return manageDefaultTheme()
	case "mono", "plain", "none", "no-color":
		return manageTheme{}
	case "contrast", "high-contrast":
		return manageContrastTheme()
	default:
		return manageDefaultTheme()
	}
}

func manageDefaultTheme() manageTheme {
	return manageTheme{
		title:        "\033[1;38;2;203;166;247m",
		subtitle:     "\033[38;2;166;173;200m",
		help:         "\033[38;2;127;132;156m",
		muted:        "\033[38;2;147;153;178m",
		tableHeader:  "\033[1;38;2;205;214;244;48;2;49;50;68m",
		search:       "\033[38;2;147;153;178m",
		searchActive: "\033[1;38;2;148;226;213m",
		selected:     "\033[1;38;2;30;30;46;48;2;137;180;250m",
		warning:      "\033[38;2;250;179;135m",
		dialog:       "\033[38;2;205;214;244;48;2;49;50;68m",
		dialogTitle:  "\033[1;38;2;30;30;46;48;2;203;166;247m",
		dialogHelp:   "\033[38;2;137;220;235;48;2;49;50;68m",
		footer:       "\033[38;2;166;173;200m",
	}
}

func manageContrastTheme() manageTheme {
	return manageTheme{
		title:        "\033[1;38;2;245;224;220m",
		subtitle:     "\033[38;2;205;214;244m",
		help:         "\033[38;2;186;194;222m",
		muted:        "\033[38;2;186;194;222m",
		tableHeader:  "\033[1;38;2;17;17;27;48;2;245;224;220m",
		search:       "\033[38;2;186;194;222m",
		searchActive: "\033[1;38;2;166;227;161m",
		selected:     "\033[1;38;2;17;17;27;48;2;166;227;161m",
		warning:      "\033[1;38;2;249;226;175m",
		dialog:       "\033[38;2;205;214;244;48;2;30;30;46m",
		dialogTitle:  "\033[1;38;2;17;17;27;48;2;166;227;161m",
		dialogHelp:   "\033[38;2;249;226;175;48;2;30;30;46m",
		footer:       "\033[38;2;205;214;244m",
	}
}

func themed(style, text string) string {
	if style == "" {
		return text
	}
	return style + text + ansiReset
}

type manageMode int

const (
	modeList manageMode = iota
	modeSearch
	modeManagers
	modeDialog
	modeInput
)

type manageInputKind int

const (
	inputNone manageInputKind = iota
	inputInstallManager
	inputInstallPackage
	inputVersion
)

type manageUI struct {
	inv             packageInventory
	filtered        []installedPackage
	selected        int
	offset          int
	search          string
	managerOptions  []string
	managerEnabled  map[string]bool
	managerSelected int
	mode            manageMode
	inputKind       manageInputKind
	inputLabel      string
	inputValue      string
	installManager  string
	pendingAction   packageAction
	pendingPackage  installedPackage
	message         string
	loading         bool
}

type manageTerminal struct {
	saved string
	raw   bool
	input *os.File
}

func (t manageTerminal) restore() { t.suspend() }

func (t manageTerminal) suspend() {
	if !t.raw || t.saved == "" {
		return
	}
	cmd := exec.Command("stty", t.saved)
	cmd.Stdin = t.input
	_ = cmd.Run()
}

func (t manageTerminal) resume() {
	if !t.raw {
		return
	}
	cmd := exec.Command("stty", "-icanon", "-echo", "min", "1", "time", "0")
	cmd.Stdin = t.input
	_ = cmd.Run()
}

func handlePackageInventory(stdout, stderr io.Writer) int {
	inv := collectPackageInventory(manager.All())
	renderPackageInventory(stdout, inv)
	return 0
}

func handleManage(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return handlePackageTUI(stdout, stderr)
	}
	if args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "usage: pre manage [--package <name> --manager <mgr> --install|--upgrade [version]|--downgrade <version>|--uninstall]")
		return 0
	}
	if args[0] == "--list" || args[0] == "list" {
		return handlePackageInventory(stdout, stderr)
	}
	if !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "install":
			return handlePackageAction(actionInstall, args[1:], stdout, stderr)
		case "update", "upgrade":
			return handlePackageAction(actionUpdate, args[1:], stdout, stderr)
		case "downgrade":
			return handlePackageAction(actionDowngrade, args[1:], stdout, stderr)
		case "uninstall", "remove":
			return handlePackageAction(actionUninstall, args[1:], stdout, stderr)
		default:
			fmt.Fprintln(stderr, "usage: pre manage [--package <name> --manager <mgr> --install|--upgrade [version]|--downgrade <version>|--uninstall]")
			return 1
		}
	}

	req, err := packageActionRequestFromManageFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "pre manage: %v\n", err)
		return 1
	}
	if err := executePackageAction(req, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "pre manage: %v\n", err)
		return 1
	}
	return 0
}

func handlePackageTUI(stdout, stderr io.Writer) int {
	input, closeInput, err := openManageInput()
	if err != nil {
		fmt.Fprintln(stderr, "pre manage: interactive terminal required; run from a terminal or use `pre installed` / `pre manage --package ...`")
		return 1
	}
	defer closeInput()

	term := enableManageRawMode(input, stderr)
	defer term.restore()
	fmt.Fprint(stdout, ansiAltScreen+ansiHideCursor+ansiClear)
	screenActive := true
	closeScreen := func() {
		if !screenActive {
			return
		}
		fmt.Fprint(stdout, ansiShowCursor+ansiReset+ansiMainScreen)
		screenActive = false
	}
	defer closeScreen()

	ui := newLoadingManageUI()
	renderManageUI(stdout, &ui)
	ui.setInventory(collectPackageInventory(manager.All()))
	renderManageUI(stdout, &ui)
	for {
		key, err := readManageKey(input)
		if err != nil {
			closeScreen()
			fmt.Fprintf(stderr, "pre manage: input closed: %v\n", err)
			return 1
		}
		if handleManageKey(key, &ui, term, stdout, stderr) {
			fmt.Fprint(stdout, ansiClear)
			closeScreen()
			return 0
		}
		renderManageUI(stdout, &ui)
	}
}

func openManageInput() (io.Reader, func(), error) {
	if packageInputReader != os.Stdin {
		return packageInputReader, func() {}, nil
	}
	if isTerminalFile(os.Stdin) && hasTerminalState(os.Stdin) {
		return os.Stdin, func() {}, nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	if !isTerminalFile(tty) || !hasTerminalState(tty) {
		_ = tty.Close()
		return nil, nil, errors.New("/dev/tty is not an interactive terminal")
	}
	return tty, func() { _ = tty.Close() }, nil
}

func enableManageRawMode(input io.Reader, stderr io.Writer) manageTerminal {
	file, ok := input.(*os.File)
	if !ok || !isTerminalFile(file) {
		return manageTerminal{}
	}
	out, err := terminalState(file)
	if err != nil {
		fmt.Fprintln(stderr, "pre manage: raw terminal unavailable; continuing without raw mode")
		return manageTerminal{}
	}
	term := manageTerminal{saved: strings.TrimSpace(string(out)), raw: true, input: file}
	term.resume()
	return term
}

func isTerminalFile(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func hasTerminalState(file *os.File) bool {
	_, err := terminalState(file)
	return err == nil
}

func terminalState(file *os.File) ([]byte, error) {
	cmd := exec.Command("stty", "-g")
	cmd.Stdin = file
	return cmd.Output()
}

func newManageUI(inv packageInventory) manageUI {
	ui := manageUI{inv: inv}
	ui.setInventory(inv)
	return ui
}

func newLoadingManageUI() manageUI {
	ui := manageUI{loading: true, message: loadingManageMessage}
	ui.applyFilter()
	return ui
}

func (ui *manageUI) setInventory(inv packageInventory) {
	ui.inv = inv
	ui.loading = false
	if ui.message == loadingManageMessage {
		ui.message = ""
	}
	ui.syncManagerOptions()
	ui.applyFilter()
}

func (ui *manageUI) syncManagerOptions() {
	seen := make(map[string]bool)
	for _, pkg := range ui.inv.Packages {
		if pkg.Manager != "" {
			seen[pkg.Manager] = true
		}
	}
	options := make([]string, 0, len(seen))
	for name := range seen {
		options = append(options, name)
	}
	sort.Strings(options)

	if ui.managerEnabled == nil {
		ui.managerEnabled = make(map[string]bool, len(options))
	}
	for _, name := range options {
		if _, ok := ui.managerEnabled[name]; !ok {
			ui.managerEnabled[name] = true
		}
	}
	for name := range ui.managerEnabled {
		if !seen[name] {
			delete(ui.managerEnabled, name)
		}
	}

	ui.managerOptions = options
	if ui.managerSelected >= len(ui.managerOptions) {
		ui.managerSelected = len(ui.managerOptions) - 1
	}
	if ui.managerSelected < 0 {
		ui.managerSelected = 0
	}
}

func (ui *manageUI) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(ui.search))
	ui.filtered = ui.filtered[:0]
	for _, pkg := range ui.inv.Packages {
		if !ui.managerEnabled[pkg.Manager] {
			continue
		}
		if query == "" || packageMatchesQuery(pkg, query) {
			ui.filtered = append(ui.filtered, pkg)
		}
	}
	if ui.selected >= len(ui.filtered) {
		ui.selected = len(ui.filtered) - 1
	}
	if ui.selected < 0 {
		ui.selected = 0
	}
	if ui.offset > ui.selected {
		ui.offset = ui.selected
	}
	if ui.offset < 0 {
		ui.offset = 0
	}
}

func packageMatchesQuery(pkg installedPackage, query string) bool {
	return strings.Contains(strings.ToLower(pkg.Manager), query) ||
		strings.Contains(strings.ToLower(pkg.Name), query) ||
		strings.Contains(strings.ToLower(pkg.Version), query) ||
		strings.Contains(strings.ToLower(pkg.Ecosystem), query)
}

func (ui manageUI) managerSummary() string {
	if len(ui.managerOptions) == 0 {
		return "none"
	}
	var enabled []string
	for _, name := range ui.managerOptions {
		if ui.managerEnabled[name] {
			enabled = append(enabled, name)
		}
	}
	switch {
	case len(enabled) == 0:
		return "none"
	case len(enabled) == len(ui.managerOptions):
		return "all"
	case len(enabled) <= 3:
		return strings.Join(enabled, ",")
	default:
		return fmt.Sprintf("%d/%d", len(enabled), len(ui.managerOptions))
	}
}

func (ui manageUI) managerPackageCounts() map[string]int {
	counts := make(map[string]int, len(ui.managerOptions))
	for _, pkg := range ui.inv.Packages {
		counts[pkg.Manager]++
	}
	return counts
}

func renderManageUI(stdout io.Writer, ui *manageUI) {
	theme := currentManageTheme()
	width, height := terminalSizeFn()
	width, height = normalizeTerminalSize(width, height)
	dialogLines := manageDialogLines(*ui, width)
	warnings := warningLines(ui.inv.Errors, width)
	reservedLines := 5 + len(warnings) + len(dialogLines)
	pageSize := height - reservedLines
	if pageSize < 1 {
		pageSize = 1
	}
	ui.ensureSelectionVisible(pageSize)

	fmt.Fprint(stdout, ansiClear)
	printPadded(stdout, themed(theme.title, "pre manage")+" "+themed(theme.subtitle, "package lifecycle"), width)
	help := "↑/k ↓/j navigate  / live filter  m managers  space/enter actions  i install  u upgrade  d downgrade  r remove  q quit"
	printPadded(stdout, themed(theme.help, fitLine(help, width)), width)
	renderSearchLine(stdout, *ui, width)
	printPadded(stdout, themed(theme.tableHeader, manageTableHeader(width)), width)

	if len(ui.filtered) == 0 {
		emptyMessage := "  no packages found"
		if ui.loading {
			emptyMessage = "  " + loadingManageMessage
		}
		printPadded(stdout, themed(theme.muted, fitLine(emptyMessage, width)), width)
		for i := 1; i < pageSize; i++ {
			printPadded(stdout, "", width)
		}
	} else {
		end := ui.offset + pageSize
		if end > len(ui.filtered) {
			end = len(ui.filtered)
		}
		for i := ui.offset; i < end; i++ {
			line := managePackageLine(i, ui.filtered[i], width, i == ui.selected)
			if i == ui.selected {
				printPadded(stdout, themed(theme.selected, line), width)
			} else {
				printPadded(stdout, line, width)
			}
		}
		for i := end - ui.offset; i < pageSize; i++ {
			printPadded(stdout, "", width)
		}
	}

	for _, line := range warnings {
		printPadded(stdout, themed(theme.warning, line), width)
	}

	for _, line := range dialogLines {
		printPadded(stdout, line, width)
	}

	printPadded(stdout, manageFooterLine(*ui, pageSize, width), width)
}

func manageDialogLines(ui manageUI, width int) []string {
	switch ui.mode {
	case modeSearch:
		return searchDialogLines(ui, width)
	case modeManagers:
		return managerDialogLines(ui, width)
	case modeDialog:
		return actionDialogLines(ui, width)
	case modeInput:
		return inputDialogLines(ui, width)
	default:
		return nil
	}
}

func renderSearchLine(stdout io.Writer, ui manageUI, width int) {
	theme := currentManageTheme()
	prefix := "search"
	color := theme.search
	if ui.mode == modeSearch {
		prefix = "/"
		color = theme.searchActive
	}
	query := ui.search
	if query == "" {
		query = "type / to filter"
		color = theme.search
	}
	line := fmt.Sprintf("%s %s   managers %s", prefix, query, ui.managerSummary())
	printPadded(stdout, themed(color, fitLine(line, width)), width)
}

func actionDialogLines(ui manageUI, width int) []string {
	theme := currentManageTheme()
	pkg, ok := ui.currentPackage()
	if !ok {
		return nil
	}
	return []string{
		themed(theme.dialogTitle, fitLine(" actions", width)),
		themed(theme.dialog, fitLine(" "+pkg.Manager+"  "+pkg.Name, width)),
		themed(theme.dialog, fitLine(" version: "+emptyDash(pkg.Version), width)),
		themed(theme.dialogHelp, fitLine(" [u] upgrade   [d] downgrade   [r] uninstall   [i] install   [space/x/esc] close", width)),
	}
}

func searchDialogLines(ui manageUI, width int) []string {
	theme := currentManageTheme()
	return []string{
		themed(theme.dialogTitle, fitLine(" search", width)),
		themed(theme.dialog, fitLine(" /"+ui.search, width)),
		themed(theme.dialogHelp, fitLine(" live filter   / or esc close   backspace edit   q quit", width)),
	}
}

func managerDialogLines(ui manageUI, width int) []string {
	theme := currentManageTheme()
	lines := []string{
		themed(theme.dialogTitle, fitLine(" managers", width)),
		themed(theme.dialogHelp, fitLine(" ↑/k ↓/j move   space/enter toggle   a all   x/esc close   q quit", width)),
	}
	if len(ui.managerOptions) == 0 {
		return append(lines, themed(theme.dialog, fitLine(" none found", width)))
	}

	counts := ui.managerPackageCounts()
	const maxVisible = 8
	start := ui.managerSelected - maxVisible/2
	if start < 0 {
		start = 0
	}
	if start+maxVisible > len(ui.managerOptions) {
		start = len(ui.managerOptions) - maxVisible
	}
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > len(ui.managerOptions) {
		end = len(ui.managerOptions)
	}
	if start > 0 {
		lines = append(lines, themed(theme.dialogHelp, fitLine(" ↑ more", width)))
	}
	for i := start; i < end; i++ {
		name := ui.managerOptions[i]
		box := "[ ]"
		if ui.managerEnabled[name] {
			box = "[x]"
		}
		marker := " "
		if i == ui.managerSelected {
			marker = "→"
		}
		line := fitLine(fmt.Sprintf(" %s %s %-12s %d packages", marker, box, name, counts[name]), width)
		if i == ui.managerSelected {
			lines = append(lines, themed(theme.selected, line))
		} else {
			lines = append(lines, themed(theme.dialog, line))
		}
	}
	if end < len(ui.managerOptions) {
		lines = append(lines, themed(theme.dialogHelp, fitLine(" ↓ more", width)))
	}
	return lines
}

func inputDialogLines(ui manageUI, width int) []string {
	theme := currentManageTheme()
	return []string{
		themed(theme.dialogTitle, fitLine(" "+ui.inputLabel, width)),
		themed(theme.dialog, fitLine(" "+ui.inputValue, width)),
		themed(theme.dialogHelp, fitLine(" enter confirm   esc cancel", width)),
	}
}

func handleManageKey(key int, ui *manageUI, term manageTerminal, stdout, stderr io.Writer) bool {
	if key == keyCtrlC {
		return true
	}
	switch ui.mode {
	case modeSearch:
		return handleSearchKey(key, ui)
	case modeManagers:
		return handleManagerKey(key, ui)
	case modeDialog:
		return handleDialogKey(key, ui, term, stdout, stderr)
	case modeInput:
		return handleInputKey(key, ui, term, stdout, stderr)
	default:
		return handleListKey(key, ui, term, stdout, stderr)
	}
}

func handleListKey(key int, ui *manageUI, term manageTerminal, stdout, stderr io.Writer) bool {
	switch key {
	case 'q':
		return true
	case keyUp, 'k':
		ui.moveSelection(-1)
	case keyDown, 'j':
		ui.moveSelection(1)
	case '/':
		ui.toggleSearch()
	case 'm':
		ui.toggleManagers()
	case keyEnter, ' ', 'o':
		ui.toggleDialog()
	case 'i':
		ui.beginInput(inputInstallManager, "manager")
	case 'u':
		ui.runSelectedAction(actionUpdate, "", term, stdout, stderr)
	case 'd':
		ui.beginVersionInput(actionDowngrade)
	case 'r':
		ui.runSelectedAction(actionUninstall, "", term, stdout, stderr)
	}
	return false
}

func handleSearchKey(key int, ui *manageUI) bool {
	switch key {
	case 'q':
		return true
	case keyEsc, keyEnter, '/':
		ui.mode = modeList
	case keyBackspace:
		if len(ui.search) > 0 {
			ui.search = ui.search[:len(ui.search)-1]
			ui.applyFilter()
		}
	case keyUp, 'k':
		ui.moveSelection(-1)
	case keyDown, 'j':
		ui.moveSelection(1)
	default:
		if key >= 32 && key < 127 {
			ui.search += string(rune(key))
			ui.applyFilter()
		}
	}
	return false
}

func handleManagerKey(key int, ui *manageUI) bool {
	switch key {
	case 'q':
		return true
	case keyEsc, 'x', 'm':
		ui.mode = modeList
	case keyUp, 'k':
		ui.moveManagerSelection(-1)
	case keyDown, 'j':
		ui.moveManagerSelection(1)
	case keyEnter, ' ':
		ui.toggleSelectedManager()
	case 'a':
		ui.enableAllManagers()
	}
	return false
}

func handleDialogKey(key int, ui *manageUI, term manageTerminal, stdout, stderr io.Writer) bool {
	switch key {
	case 'q':
		return true
	case keyEsc, 'x', ' ', keyEnter, 'o':
		ui.mode = modeList
	case 'u':
		ui.runSelectedAction(actionUpdate, "", term, stdout, stderr)
	case 'd':
		ui.beginVersionInput(actionDowngrade)
	case 'r':
		ui.runSelectedAction(actionUninstall, "", term, stdout, stderr)
	case 'i':
		ui.runSelectedAction(actionInstall, "", term, stdout, stderr)
	}
	return false
}

func (ui *manageUI) toggleSearch() {
	if ui.mode == modeSearch {
		ui.mode = modeList
		return
	}
	ui.mode = modeSearch
	ui.message = ""
}

func (ui *manageUI) toggleManagers() {
	if ui.mode == modeManagers {
		ui.mode = modeList
		return
	}
	ui.mode = modeManagers
	ui.message = ""
}

func (ui *manageUI) toggleDialog() {
	if ui.mode == modeDialog {
		ui.mode = modeList
		return
	}
	if _, ok := ui.currentPackage(); ok {
		ui.mode = modeDialog
	}
}

func handleInputKey(key int, ui *manageUI, term manageTerminal, stdout, stderr io.Writer) bool {
	switch key {
	case keyEsc:
		ui.mode = modeList
		ui.inputValue = ""
	case keyBackspace:
		if len(ui.inputValue) > 0 {
			ui.inputValue = ui.inputValue[:len(ui.inputValue)-1]
		}
	case keyEnter:
		ui.submitInput(term, stdout, stderr)
	default:
		if key >= 32 && key < 127 {
			ui.inputValue += string(rune(key))
		}
	}
	return false
}

func (ui *manageUI) moveSelection(delta int) {
	if len(ui.filtered) == 0 {
		return
	}
	ui.selected += delta
	if ui.selected < 0 {
		ui.selected = 0
	}
	if ui.selected >= len(ui.filtered) {
		ui.selected = len(ui.filtered) - 1
	}
}

func (ui *manageUI) moveManagerSelection(delta int) {
	if len(ui.managerOptions) == 0 {
		return
	}
	ui.managerSelected += delta
	if ui.managerSelected < 0 {
		ui.managerSelected = 0
	}
	if ui.managerSelected >= len(ui.managerOptions) {
		ui.managerSelected = len(ui.managerOptions) - 1
	}
}

func (ui *manageUI) toggleSelectedManager() {
	if len(ui.managerOptions) == 0 {
		return
	}
	name := ui.managerOptions[ui.managerSelected]
	ui.managerEnabled[name] = !ui.managerEnabled[name]
	ui.selected = 0
	ui.offset = 0
	ui.applyFilter()
}

func (ui *manageUI) enableAllManagers() {
	for _, name := range ui.managerOptions {
		ui.managerEnabled[name] = true
	}
	ui.selected = 0
	ui.offset = 0
	ui.applyFilter()
}

func (ui *manageUI) ensureSelectionVisible(pageSize int) {
	if pageSize < 1 {
		pageSize = 1
	}
	if len(ui.filtered) == 0 {
		ui.selected = 0
		ui.offset = 0
		return
	}
	if ui.selected < 0 {
		ui.selected = 0
	}
	if ui.selected >= len(ui.filtered) {
		ui.selected = len(ui.filtered) - 1
	}
	if ui.offset > ui.selected {
		ui.offset = ui.selected
	}
	if ui.selected >= ui.offset+pageSize {
		ui.offset = ui.selected - pageSize + 1
	}
	maxOffset := len(ui.filtered) - pageSize
	if maxOffset < 0 {
		maxOffset = 0
	}
	if ui.offset > maxOffset {
		ui.offset = maxOffset
	}
	if ui.offset < 0 {
		ui.offset = 0
	}
}

func (ui *manageUI) currentPackage() (installedPackage, bool) {
	if ui.selected < 0 || ui.selected >= len(ui.filtered) {
		return installedPackage{}, false
	}
	return ui.filtered[ui.selected], true
}

func (ui *manageUI) beginInput(kind manageInputKind, label string) {
	ui.mode = modeInput
	ui.inputKind = kind
	ui.inputLabel = label
	ui.inputValue = ""
	ui.message = ""
}

func (ui *manageUI) beginVersionInput(action packageAction) {
	pkg, ok := ui.currentPackage()
	if !ok {
		return
	}
	ui.pendingPackage = pkg
	ui.pendingAction = action
	ui.beginInput(inputVersion, "version")
}

func (ui *manageUI) submitInput(term manageTerminal, stdout, stderr io.Writer) {
	value := strings.TrimSpace(ui.inputValue)
	switch ui.inputKind {
	case inputInstallManager:
		if value == "" {
			ui.message = "manager is required"
			return
		}
		ui.installManager = value
		ui.beginInput(inputInstallPackage, "package")
	case inputInstallPackage:
		if value == "" {
			ui.message = "package is required"
			return
		}
		mgr := manager.Get(ui.installManager)
		if mgr == nil {
			ui.beginInput(inputInstallManager, "manager")
			ui.message = "unknown manager: " + ui.installManager
			return
		}
		req := packageActionReq{Action: actionInstall, Manager: mgr, Package: value}
		ui.runAction(req, term, stdout, stderr)
	case inputVersion:
		if value == "" {
			ui.message = "version is required"
			return
		}
		pkg := ui.pendingPackage
		mgr := manager.Get(pkg.Manager)
		if mgr == nil {
			ui.message = "unknown manager: " + pkg.Manager
			ui.mode = modeList
			return
		}
		req := packageActionReq{Action: ui.pendingAction, Manager: mgr, Package: pkg.Name, Version: value}
		ui.runAction(req, term, stdout, stderr)
	}
}

func (ui *manageUI) runSelectedAction(action packageAction, version string, term manageTerminal, stdout, stderr io.Writer) {
	pkg, ok := ui.currentPackage()
	if !ok {
		return
	}
	mgr := manager.Get(pkg.Manager)
	if mgr == nil {
		ui.message = "unknown manager: " + pkg.Manager
		return
	}
	req := packageActionReq{Action: action, Manager: mgr, Package: pkg.Name, Version: version}
	ui.runAction(req, term, stdout, stderr)
}

func (ui *manageUI) runAction(req packageActionReq, term manageTerminal, stdout, stderr io.Writer) {
	args, err := buildPackageManagerArgs(req)
	if err != nil {
		ui.message = err.Error()
		ui.mode = modeList
		return
	}
	term.suspend()
	fmt.Fprint(stdout, ansiShowCursor+ansiReset+ansiClear)
	fmt.Fprintf(stdout, "running: pre %s %s\n\n", req.Manager.Name, strings.Join(args, " "))
	err = executePackageAction(req, stdout, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "pre manage: %v\n", err)
		ui.message = err.Error()
	} else {
		ui.message = fmt.Sprintf("%s %s", req.Action, req.Package)
	}
	manageActionPauseFn()
	term.resume()
	fmt.Fprint(stdout, ansiHideCursor)
	ui.setInventory(collectPackageInventory(manager.All()))
	ui.mode = modeList
	ui.inputValue = ""
}

func readManageKey(r io.Reader) (int, error) {
	b, err := readByteBlocking(r)
	if err != nil {
		return 0, err
	}
	switch b {
	case 3:
		return keyCtrlC, nil
	case '\r', '\n':
		return keyEnter, nil
	case 27:
		return readEscapeKey(r), nil
	case 127, 8:
		return keyBackspace, nil
	default:
		return int(b), nil
	}
}

func readEscapeKey(r io.Reader) int {
	if file, ok := r.(*os.File); ok {
		_ = syscall.SetNonblock(int(file.Fd()), true)
		defer syscall.SetNonblock(int(file.Fd()), false)
	}
	b1, ok := readByteOptional(r)
	if !ok {
		return keyEsc
	}
	if b1 != '[' {
		return keyEsc
	}
	b2, ok := readByteOptional(r)
	if !ok {
		return keyEsc
	}
	switch b2 {
	case 'A':
		return keyUp
	case 'B':
		return keyDown
	default:
		return keyEsc
	}
}

func readByteBlocking(r io.Reader) (byte, error) {
	buf := []byte{0}
	for {
		n, err := r.Read(buf)
		if n > 0 {
			return buf[0], nil
		}
		if err != nil {
			if retryableReadError(err) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return 0, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readByteOptional(r io.Reader) (byte, bool) {
	deadline := time.Now().Add(40 * time.Millisecond)
	buf := []byte{0}
	for time.Now().Before(deadline) {
		n, err := r.Read(buf)
		if n > 0 {
			return buf[0], true
		}
		if err != nil {
			if retryableReadError(err) {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			return 0, false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return 0, false
}

func retryableReadError(err error) bool {
	return errors.Is(err, syscall.EAGAIN) ||
		errors.Is(err, syscall.EWOULDBLOCK) ||
		errors.Is(err, syscall.EINTR)
}

type manageFlagRequest struct {
	managerName string
	packageName string
	version     string
	action      packageAction
}

func packageActionRequestFromManageFlags(args []string) (packageActionReq, error) {
	req, err := parseManageFlags(args)
	if err != nil {
		return packageActionReq{}, err
	}
	if req.action == "" {
		return packageActionReq{}, errors.New("choose one of --install, --upgrade, --downgrade, or --uninstall")
	}
	if req.packageName == "" {
		return packageActionReq{}, errors.New("--package is required")
	}
	mgr, err := resolveManageManager(req)
	if err != nil {
		return packageActionReq{}, err
	}
	return packageActionReq{
		Action:  req.action,
		Manager: mgr,
		Package: req.packageName,
		Version: req.version,
	}, nil
}

func parseManageFlags(args []string) (manageFlagRequest, error) {
	var req manageFlagRequest
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--manager":
			val, next, err := flagValue(args, i, arg)
			if err != nil {
				return req, err
			}
			req.managerName, i = val, next
		case "--package", "-p":
			val, next, err := flagValue(args, i, arg)
			if err != nil {
				return req, err
			}
			req.packageName, i = val, next
		case "--install":
			if req.action != "" {
				return req, errors.New("choose only one package action")
			}
			req.action = actionInstall
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				req.version = args[i+1]
				i++
			}
		case "--upgrade", "--update":
			if req.action != "" {
				return req, errors.New("choose only one package action")
			}
			req.action = actionUpdate
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				req.version = args[i+1]
				i++
			}
		case "--downgrade":
			if req.action != "" {
				return req, errors.New("choose only one package action")
			}
			req.action = actionDowngrade
			val, next, err := flagValue(args, i, arg)
			if err != nil {
				return req, err
			}
			req.version, i = val, next
		case "--uninstall", "--remove":
			if req.action != "" {
				return req, errors.New("choose only one package action")
			}
			req.action = actionUninstall
		default:
			return req, fmt.Errorf("unknown option %s", arg)
		}
	}
	return req, nil
}

func flagValue(args []string, idx int, flag string) (string, int, error) {
	if idx+1 >= len(args) || strings.HasPrefix(args[idx+1], "-") {
		return "", idx, fmt.Errorf("%s requires a value", flag)
	}
	return args[idx+1], idx + 1, nil
}

func resolveManageManager(req manageFlagRequest) (*manager.Manager, error) {
	if req.managerName != "" {
		mgr := manager.Get(req.managerName)
		if mgr == nil {
			return nil, fmt.Errorf("unknown manager: %s", req.managerName)
		}
		return mgr, nil
	}
	if req.action == actionInstall {
		return nil, errors.New("--manager is required for installs")
	}
	inv := collectPackageInventory(manager.All())
	var matches []installedPackage
	for _, pkg := range inv.Packages {
		if pkg.Name == req.packageName {
			matches = append(matches, pkg)
		}
	}
	switch len(matches) {
	case 0:
		return nil, errors.New("package not found in inventory; pass --manager")
	case 1:
		return manager.Get(matches[0].Manager), nil
	default:
		var names []string
		for _, pkg := range matches {
			names = append(names, pkg.Manager)
		}
		return nil, fmt.Errorf("package is installed under multiple managers (%s); pass --manager", strings.Join(names, ", "))
	}
}

func handlePackageAction(action packageAction, args []string, stdout, stderr io.Writer) int {
	req, err := packageActionRequest(action, args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := executePackageAction(req, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "pre %s: %v\n", action, err)
		return 1
	}
	return 0
}

type packageActionReq struct {
	Action  packageAction
	Manager *manager.Manager
	Package string
	Version string
}

func packageActionRequest(action packageAction, args []string) (packageActionReq, error) {
	minArgs := 2
	if action == actionUpdate {
		minArgs = 1
	}
	if len(args) < minArgs {
		return packageActionReq{}, fmt.Errorf("usage: %s", packageActionUsage(action))
	}
	mgr := manager.Get(args[0])
	if mgr == nil {
		return packageActionReq{}, fmt.Errorf("pre %s: unknown manager: %s", action, args[0])
	}

	req := packageActionReq{Action: action, Manager: mgr}
	switch action {
	case actionInstall:
		req.Package = strings.Join(args[1:], " ")
	case actionUpdate:
		if len(args) > 1 {
			req.Package = strings.Join(args[1:], " ")
		}
	case actionUninstall:
		req.Package = strings.Join(args[1:], " ")
	case actionDowngrade:
		if len(args) < 3 {
			return packageActionReq{}, fmt.Errorf("usage: %s", packageActionUsage(action))
		}
		req.Package = args[1]
		req.Version = strings.Join(args[2:], " ")
	default:
		return packageActionReq{}, fmt.Errorf("pre: unsupported package action %q", action)
	}
	return req, nil
}

func packageActionUsage(action packageAction) string {
	switch action {
	case actionInstall:
		return "pre install <manager> <package>"
	case actionUpdate:
		return "pre update <manager> [package]"
	case actionUninstall:
		return "pre uninstall <manager> <package>"
	case actionDowngrade:
		return "pre downgrade <manager> <package> <version>"
	default:
		return "pre install|update|uninstall|downgrade ..."
	}
}

func executePackageAction(req packageActionReq, stdout, stderr io.Writer) error {
	args, err := buildPackageManagerArgs(req)
	if err != nil {
		return err
	}
	return runPreManagerCommand(req.Manager, args, stdout, stderr)
}

func buildPackageManagerArgs(req packageActionReq) ([]string, error) {
	name := packageNameOnly(req.Manager, req.Package)
	switch req.Manager.Name {
	case "brew":
		return buildBrewArgs(req, name)
	case "npm":
		return buildNPMArgs(req, name, "install", "uninstall")
	case "pnpm":
		return buildNPMArgs(req, name, "add", "remove")
	case "bun":
		return buildNPMArgs(req, name, "add", "remove")
	case "go":
		return buildGoArgs(req, name)
	case "pip", "pip3":
		return buildPipArgs(req, name)
	case "uv":
		return buildUVArgs(req, name)
	case "poetry":
		return buildPoetryArgs(req, name)
	default:
		return buildGenericPackageArgs(req)
	}
}

func buildBrewArgs(req packageActionReq, name string) ([]string, error) {
	switch req.Action {
	case actionInstall:
		return []string{"install", packageWithVersion(req.Manager, req.Package, req.Version)}, nil
	case actionUpdate:
		if req.Version != "" && name != "" {
			return []string{"install", name + "@" + req.Version}, nil
		}
		if name == "" {
			return []string{"upgrade"}, nil
		}
		return []string{"upgrade", name}, nil
	case actionUninstall:
		return []string{"uninstall", name}, nil
	case actionDowngrade:
		return []string{"install", name + "@" + req.Version}, nil
	}
	return nil, unsupportedPackageAction(req)
}

func buildNPMArgs(req packageActionReq, name, installCmd, removeCmd string) ([]string, error) {
	switch req.Action {
	case actionInstall:
		return []string{installCmd, packageWithVersion(req.Manager, req.Package, req.Version)}, nil
	case actionUpdate:
		if name == "" {
			return []string{"update"}, nil
		}
		if req.Version != "" {
			return []string{installCmd, name + "@" + req.Version}, nil
		}
		return []string{installCmd, name + "@latest"}, nil
	case actionUninstall:
		return []string{removeCmd, name}, nil
	case actionDowngrade:
		return []string{installCmd, name + "@" + req.Version}, nil
	}
	return nil, unsupportedPackageAction(req)
}

func buildGoArgs(req packageActionReq, name string) ([]string, error) {
	switch req.Action {
	case actionInstall:
		return []string{"get", packageWithVersion(req.Manager, req.Package, req.Version)}, nil
	case actionUpdate:
		if name == "" {
			return []string{"get", "-u", "./..."}, nil
		}
		if req.Version != "" {
			return []string{"get", name + "@" + req.Version}, nil
		}
		return []string{"get", name + "@latest"}, nil
	case actionUninstall:
		return []string{"get", name + "@none"}, nil
	case actionDowngrade:
		return []string{"get", name + "@" + req.Version}, nil
	}
	return nil, unsupportedPackageAction(req)
}

func buildPipArgs(req packageActionReq, name string) ([]string, error) {
	switch req.Action {
	case actionInstall:
		return []string{"install", packageWithVersion(req.Manager, req.Package, req.Version)}, nil
	case actionUpdate:
		if name == "" {
			return nil, errors.New("pip updates require a package name")
		}
		if req.Version != "" {
			return []string{"install", "--upgrade", name + "==" + req.Version}, nil
		}
		return []string{"install", "--upgrade", name}, nil
	case actionUninstall:
		return []string{"uninstall", "-y", name}, nil
	case actionDowngrade:
		return []string{"install", name + "==" + req.Version}, nil
	}
	return nil, unsupportedPackageAction(req)
}

func buildUVArgs(req packageActionReq, name string) ([]string, error) {
	switch req.Action {
	case actionInstall:
		return []string{"add", packageWithVersion(req.Manager, req.Package, req.Version)}, nil
	case actionUpdate:
		if name == "" {
			return nil, errors.New("uv updates require a package name")
		}
		if req.Version != "" {
			return []string{"add", name + "==" + req.Version}, nil
		}
		return []string{"add", name}, nil
	case actionUninstall:
		return []string{"remove", name}, nil
	case actionDowngrade:
		return []string{"add", name + "==" + req.Version}, nil
	}
	return nil, unsupportedPackageAction(req)
}

func buildPoetryArgs(req packageActionReq, name string) ([]string, error) {
	switch req.Action {
	case actionInstall:
		return []string{"add", packageWithVersion(req.Manager, req.Package, req.Version)}, nil
	case actionUpdate:
		if name == "" {
			return []string{"update"}, nil
		}
		if req.Version != "" {
			return []string{"add", name + "@" + req.Version}, nil
		}
		return []string{"add", name + "@latest"}, nil
	case actionUninstall:
		return []string{"remove", name}, nil
	case actionDowngrade:
		return []string{"add", name + "@" + req.Version}, nil
	}
	return nil, unsupportedPackageAction(req)
}

func buildGenericPackageArgs(req packageActionReq) ([]string, error) {
	switch req.Action {
	case actionInstall:
		cmd := "install"
		if len(req.Manager.InstallCmds) > 0 {
			cmd = req.Manager.InstallCmds[0]
		}
		return []string{cmd, req.Package}, nil
	case actionUpdate:
		if managerSupportsCommand(req.Manager, "update") {
			if req.Package == "" {
				return []string{"update"}, nil
			}
			return []string{"update", req.Package}, nil
		}
	}
	return nil, unsupportedPackageAction(req)
}

func unsupportedPackageAction(req packageActionReq) error {
	return fmt.Errorf("%s does not support %s through pre yet", req.Manager.Name, req.Action)
}

func managerSupportsCommand(mgr *manager.Manager, cmd string) bool {
	if mgr == nil {
		return false
	}
	for _, installCmd := range mgr.InstallCmds {
		if installCmd == cmd {
			return true
		}
	}
	return false
}

func packageNameOnly(mgr *manager.Manager, spec string) string {
	name, _ := manager.ParseSpec(mgr.Ecosystem, spec)
	return strings.TrimSpace(name)
}

func packageWithVersion(mgr *manager.Manager, spec, version string) string {
	spec = strings.TrimSpace(spec)
	version = strings.TrimSpace(version)
	if version == "" || spec == "" {
		return spec
	}
	name := packageNameOnly(mgr, spec)
	if name == "" {
		name = spec
	}
	switch mgr.Ecosystem {
	case "PyPI":
		return name + "==" + version
	case "Homebrew":
		return name + "@" + version
	default:
		return name + "@" + version
	}
}

func runPreManagerCommand(mgr *manager.Manager, args []string, stdout, stderr io.Writer) error {
	self, err := executablePathFn()
	if err != nil || self == "" {
		self = "pre"
	}
	preArgs := append([]string{mgr.Name}, args...)
	return commandRunnerFn(self, preArgs, nil, stdout, stderr)
}

func collectPackageInventory(mgrs []manager.Manager) packageInventory {
	type inventoryResult struct {
		inv packageInventory
	}

	results := make(chan inventoryResult, len(mgrs))
	var wg sync.WaitGroup

	for _, mgr := range mgrs {
		mgr := mgr
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := lookPathFn(mgr.Name); err != nil {
				return
			}

			var partial packageInventory
			pkgs, err := listInstalledPackages(&mgr)
			if err != nil {
				if fallback := manifestPackages(&mgr); len(fallback) > 0 {
					partial.Packages = append(partial.Packages, fallback...)
					partial.Errors = append(partial.Errors, fmt.Sprintf("%s: package manager list failed; showing project manifest/lockfile", mgr.Name))
				}
				results <- inventoryResult{inv: partial}
				return
			}
			partial.Packages = pkgs
			results <- inventoryResult{inv: partial}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var inv packageInventory
	for result := range results {
		inv.Packages = append(inv.Packages, result.inv.Packages...)
		inv.Errors = append(inv.Errors, result.inv.Errors...)
	}
	sortPackages(inv.Packages)
	return inv
}

func listInstalledPackages(mgr *manager.Manager) ([]installedPackage, error) {
	switch mgr.Name {
	case "brew":
		if pkgs := readHomebrewPackages(mgr); len(pkgs) > 0 {
			return pkgs, nil
		}
		out, err := commandOutputFn("brew", []string{"list", "--versions"})
		return packagesOrError(parseBrewPackages(mgr, out), err)
	case "npm":
		out, err := commandOutputFn("npm", []string{"ls", "--depth=0", "--json"})
		return packagesOrError(parseNPMJSONPackages(mgr, out), err)
	case "pnpm":
		out, err := commandOutputFn("pnpm", []string{"list", "--depth", "0", "--json"})
		return packagesOrError(parsePNPMJSONPackages(mgr, out), err)
	case "go":
		out, err := commandOutputFn("go", []string{"list", "-m", "-json", "all"})
		return packagesOrError(parseGoListPackages(mgr, out), err)
	case "pip", "pip3":
		out, err := commandOutputFn(mgr.Name, []string{"list", "--format=json"})
		return packagesOrError(parsePipJSONPackages(mgr, out), err)
	case "uv":
		out, err := commandOutputFn("uv", []string{"pip", "list", "--format=json"})
		return packagesOrError(parsePipJSONPackages(mgr, out), err)
	case "poetry":
		out, err := commandOutputFn("poetry", []string{"show", "--top-level"})
		return packagesOrError(parsePoetryShowPackages(mgr, out), err)
	default:
		return manifestPackages(mgr), nil
	}
}

func readHomebrewPackages(mgr *manager.Manager) []installedPackage {
	seen := make(map[string]bool)
	var pkgs []installedPackage
	for _, prefix := range homebrewPrefixesFn() {
		pkgs = appendHomebrewPackageDir(pkgs, seen, mgr, filepath.Join(prefix, "Cellar"))
		pkgs = appendHomebrewPackageDir(pkgs, seen, mgr, filepath.Join(prefix, "Caskroom"))
	}
	sortPackages(pkgs)
	return pkgs
}

func defaultHomebrewPrefixes() []string {
	var prefixes []string
	if prefix := strings.TrimSpace(os.Getenv("HOMEBREW_PREFIX")); prefix != "" {
		prefixes = append(prefixes, prefix)
	}
	prefixes = append(prefixes, "/opt/homebrew", "/usr/local")

	seen := make(map[string]bool, len(prefixes))
	unique := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		prefix = filepath.Clean(strings.TrimSpace(prefix))
		if prefix == "." || seen[prefix] {
			continue
		}
		seen[prefix] = true
		unique = append(unique, prefix)
	}
	return unique
}

func appendHomebrewPackageDir(dst []installedPackage, seen map[string]bool, mgr *manager.Manager, dir string) []installedPackage {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dst
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		if seen[name] {
			continue
		}
		seen[name] = true
		dst = append(dst, installedPackage{
			Manager:   mgr.Name,
			Ecosystem: mgr.Ecosystem,
			Name:      name,
			Version:   homebrewPackageVersions(filepath.Join(dir, name)),
		})
	}
	return dst
}

func homebrewPackageVersions(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		versions = append(versions, name)
	}
	sort.Strings(versions)
	return strings.Join(versions, " ")
}

func packagesOrError(pkgs []installedPackage, err error) ([]installedPackage, error) {
	if len(pkgs) > 0 {
		return pkgs, nil
	}
	return pkgs, err
}

func manifestPackages(mgr *manager.Manager) []installedPackage {
	specs := manager.ReadManifest(mgr)
	pkgs := make([]installedPackage, 0, len(specs))
	for _, spec := range specs {
		name, version := manager.ParseSpec(mgr.Ecosystem, spec)
		if name == "" {
			continue
		}
		pkgs = append(pkgs, installedPackage{
			Manager: mgr.Name, Ecosystem: mgr.Ecosystem, Name: name, Version: version,
		})
	}
	return pkgs
}

func parseBrewPackages(mgr *manager.Manager, out []byte) []installedPackage {
	var pkgs []installedPackage
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		version := ""
		if len(fields) > 1 {
			version = strings.Join(fields[1:], " ")
		}
		pkgs = append(pkgs, installedPackage{Manager: mgr.Name, Ecosystem: mgr.Ecosystem, Name: fields[0], Version: version})
	}
	return pkgs
}

func parseNPMJSONPackages(mgr *manager.Manager, out []byte) []installedPackage {
	var doc struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil
	}
	return packagesFromDependencyMap(mgr, doc.Dependencies)
}

func parsePNPMJSONPackages(mgr *manager.Manager, out []byte) []installedPackage {
	var projects []struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
		DevDependencies map[string]struct {
			Version string `json:"version"`
		} `json:"devDependencies"`
	}
	if err := json.Unmarshal(out, &projects); err == nil {
		seen := make(map[string]bool)
		var pkgs []installedPackage
		for _, project := range projects {
			pkgs = appendUniquePackages(pkgs, packagesFromDependencyMap(mgr, project.Dependencies), seen)
			pkgs = appendUniquePackages(pkgs, packagesFromDependencyMap(mgr, project.DevDependencies), seen)
		}
		return pkgs
	}
	return parseNPMJSONPackages(mgr, out)
}

func packagesFromDependencyMap(mgr *manager.Manager, deps map[string]struct {
	Version string `json:"version"`
}) []installedPackage {
	pkgs := make([]installedPackage, 0, len(deps))
	for name, dep := range deps {
		pkgs = append(pkgs, installedPackage{Manager: mgr.Name, Ecosystem: mgr.Ecosystem, Name: name, Version: dep.Version})
	}
	sortPackages(pkgs)
	return pkgs
}

func appendUniquePackages(dst, src []installedPackage, seen map[string]bool) []installedPackage {
	for _, pkg := range src {
		key := pkg.Manager + "/" + pkg.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		dst = append(dst, pkg)
	}
	return dst
}

func parseGoListPackages(mgr *manager.Manager, out []byte) []installedPackage {
	dec := json.NewDecoder(bytes.NewReader(out))
	var pkgs []installedPackage
	for {
		var mod struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
			Main    bool   `json:"Main"`
		}
		if err := dec.Decode(&mod); err != nil {
			break
		}
		if mod.Main || mod.Path == "" {
			continue
		}
		pkgs = append(pkgs, installedPackage{Manager: mgr.Name, Ecosystem: mgr.Ecosystem, Name: mod.Path, Version: mod.Version})
	}
	return pkgs
}

func parsePipJSONPackages(mgr *manager.Manager, out []byte) []installedPackage {
	var rows []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil
	}
	pkgs := make([]installedPackage, 0, len(rows))
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		pkgs = append(pkgs, installedPackage{Manager: mgr.Name, Ecosystem: mgr.Ecosystem, Name: row.Name, Version: row.Version})
	}
	sortPackages(pkgs)
	return pkgs
}

func parsePoetryShowPackages(mgr *manager.Manager, out []byte) []installedPackage {
	var pkgs []installedPackage
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		pkgs = append(pkgs, installedPackage{Manager: mgr.Name, Ecosystem: mgr.Ecosystem, Name: fields[0], Version: fields[1]})
	}
	return pkgs
}

func sortPackages(pkgs []installedPackage) {
	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Manager != pkgs[j].Manager {
			return pkgs[i].Manager < pkgs[j].Manager
		}
		return pkgs[i].Name < pkgs[j].Name
	})
}

func renderPackageInventory(stdout io.Writer, inv packageInventory) {
	fmt.Fprintln(stdout, "installed packages:")
	if len(inv.Packages) == 0 {
		fmt.Fprintln(stdout, "  none found")
	} else {
		for i, pkg := range inv.Packages {
			fmt.Fprintf(stdout, "  %3d  %-8s %-36s %s\n", i+1, pkg.Manager, pkg.Name, emptyDash(pkg.Version))
		}
	}
	for _, msg := range inv.Errors {
		fmt.Fprintf(stdout, "  warning: %s\n", msg)
	}
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func detectTerminalSize() (int, int) {
	cols, hasCols := envInt("COLUMNS")
	rows, hasRows := envInt("LINES")
	if hasCols && hasRows {
		return cols, rows
	}
	if packageInputReader == os.Stdin {
		if info, err := os.Stdin.Stat(); err == nil && (info.Mode()&os.ModeCharDevice) != 0 {
			cmd := exec.Command("stty", "size")
			cmd.Stdin = os.Stdin
			out, err := cmd.Output()
			if err == nil {
				fields := strings.Fields(string(out))
				if len(fields) >= 2 {
					rows, rowErr := strconv.Atoi(fields[0])
					cols, colErr := strconv.Atoi(fields[1])
					if rowErr == nil && colErr == nil {
						return cols, rows
					}
				}
			}
		}
	}
	return 100, 30
}

func envInt(name string) (int, bool) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0, false
	}
	n, err := strconv.Atoi(value)
	return n, err == nil && n > 0
}

func normalizeTerminalSize(width, height int) (int, int) {
	if width < 40 {
		width = 40
	}
	if height < 12 {
		height = 12
	}
	return width, height
}

func manageTableHeader(width int) string {
	packageWidth, versionWidth := manageColumnWidths(width)
	return fitLine(fmt.Sprintf("  %-4s %-9s %-*s %-*s", "#", "manager", packageWidth, "package", versionWidth, "version"), width)
}

func managePackageLine(index int, pkg installedPackage, width int, selected bool) string {
	packageWidth, versionWidth := manageColumnWidths(width)
	marker := " "
	if selected {
		marker = "→"
	}
	line := fmt.Sprintf("%s %-4d %-9s %-*s %-*s",
		marker,
		index+1,
		truncate(pkg.Manager, 9),
		packageWidth,
		truncate(pkg.Name, packageWidth),
		versionWidth,
		truncate(emptyDash(pkg.Version), versionWidth),
	)
	return fitLine(line, width)
}

func manageColumnWidths(width int) (int, int) {
	versionWidth := 18
	if width < 72 {
		versionWidth = 12
	}
	packageWidth := width - 18 - versionWidth
	if packageWidth < 12 {
		packageWidth = 12
	}
	return packageWidth, versionWidth
}

func warningLines(messages []string, width int) []string {
	const maxWarnings = 2
	limit := len(messages)
	if limit > maxWarnings {
		limit = maxWarnings
	}
	lines := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		lines = append(lines, fitLine("warning: "+messages[i], width))
	}
	if len(messages) > limit {
		lines = append(lines, fitLine(fmt.Sprintf("warning: %d more manager warnings hidden", len(messages)-limit), width))
	}
	return lines
}

func manageFooterLine(ui manageUI, pageSize, width int) string {
	theme := currentManageTheme()
	parts := []string{fmt.Sprintf("%d shown / %d installed", len(ui.filtered), len(ui.inv.Packages))}
	if ui.offset > 0 {
		parts = append(parts, "↑ more")
	}
	if ui.offset+pageSize < len(ui.filtered) {
		parts = append(parts, "↓ more")
	}
	if ui.message != "" {
		parts = append(parts, ui.message)
	}
	return themed(theme.footer, fitLine(strings.Join(parts, "  "), width))
}

func printPadded(stdout io.Writer, line string, width int) {
	_ = width
	fmt.Fprintln(stdout, line)
}

func fitLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(s) > width {
		return truncate(s, width)
	}
	return padRight(s, width)
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func runCommandOutput(name string, args []string) ([]byte, error) {
	start := time.Now()
	timeout := packageListTimeout()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	debugManageTiming(name, args, start, err)
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("%s list timed out after %s", name, timeout)
	}
	return out, err
}

func debugManageTiming(name string, args []string, start time.Time, err error) {
	if os.Getenv("PRE_MANAGE_DEBUG") == "" {
		return
	}
	status := "ok"
	if err != nil {
		status = err.Error()
	}
	fmt.Fprintf(os.Stderr, "pre manage debug: %s %s took %s (%s)\n", name, strings.Join(args, " "), time.Since(start).Round(time.Millisecond), status)
}

func packageListTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv("PRE_MANAGE_LIST_TIMEOUT"))
	if value == "" {
		return 2 * time.Second
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		return 2 * time.Second
	}
	return timeout
}
