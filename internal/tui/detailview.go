package tui

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/drakeaharper/gerrit-cli/internal/config"
	"github.com/drakeaharper/gerrit-cli/internal/gerrit"
)

//go:embed gerrit-comment.vim
var gerritCommentVim string

// Pane represents different panes in DetailView
type Pane int

const (
	PaneSummary Pane = iota
	PaneDiff
	PaneReview
)

// DetailView represents the detail view with multiple panes
type DetailView struct {
	cfg    *config.Config
	keys   KeyMap
	client *gerrit.RESTClient

	// Change data
	changeID string
	change   map[string]interface{}
	comments map[string]interface{}
	files    map[string]interface{}
	fileList []string // sorted list of filenames for selection

	// Comment selection
	commentList     []commentEntry // flat ordered list of comments for selection
	selectedComment int            // selected comment index in PaneReview

	// Pane state
	activePane   Pane
	prevPane     Pane  // pane before switching to Diff
	selectedFile int // selected file index in PaneDiff

	// Cached line counts after wrap (updated in render)
	summaryLineCount int
	reviewLineCount  int

	// Scroll positions for each pane
	summaryScroll int
	diffScroll    int
	reviewScroll  int

	// Layout
	width  int
	height int

	// Loading state
	loading bool
}

// NewDetailView creates a new DetailView
func NewDetailView(cfg *config.Config, keys KeyMap, changeID string) *DetailView {
	return &DetailView{
		cfg:        cfg,
		keys:       keys,
		client:     gerrit.NewRESTClient(cfg),
		changeID:   changeID,
		activePane: PaneSummary,
		loading:    true,
	}
}

// Init implements tea.Model
func (dv *DetailView) Init() tea.Cmd {
	return dv.loadDetails()
}

// loadDetails loads change details, comments, and files
func (dv *DetailView) loadDetails() tea.Cmd {
	return func() tea.Msg {
		change, err := dv.client.GetChange(dv.changeID)
		if err != nil {
			return errMsg{err}
		}

		comments, err := dv.client.GetChangeComments(dv.changeID)
		if err != nil {
			return errMsg{err}
		}

		// Get current revision
		revision := "current"
		if revisions, ok := change["revisions"].(map[string]interface{}); ok {
			for rev := range revisions {
				revision = rev
				break
			}
		}

		files, err := dv.client.GetChangeFiles(dv.changeID, revision)
		if err != nil {
			return errMsg{err}
		}

		return detailsLoadedMsg{change, comments, files}
	}
}

// Messages
type detailsLoadedMsg struct {
	change   map[string]interface{}
	comments map[string]interface{}
	files    map[string]interface{}
}

// Update implements tea.Model
func (dv *DetailView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		dv.width = msg.Width
		dv.height = msg.Height
		return dv, nil

	case detailsLoadedMsg:
		dv.change = msg.change
		dv.comments = msg.comments
		dv.files = msg.files
		dv.loading = false
		// Build sorted file list
		dv.fileList = nil
		for filename := range dv.files {
			if filename != "/COMMIT_MSG" {
				dv.fileList = append(dv.fileList, filename)
			}
		}
		sort.Strings(dv.fileList)
		dv.selectedFile = 0
		// Build flat comment list (sorted by filename)
		dv.commentList = nil
		var commentFilenames []string
		for filename := range dv.comments {
			commentFilenames = append(commentFilenames, filename)
		}
		sort.Strings(commentFilenames)
		for _, filename := range commentFilenames {
			fileComments := dv.comments[filename]
			if commentList, ok := fileComments.([]interface{}); ok {
				for _, comment := range commentList {
					if c, ok := comment.(map[string]interface{}); ok {
						author := "Unknown"
						if authorData, ok := c["author"].(map[string]interface{}); ok {
							if name, ok := authorData["name"].(string); ok {
								author = name
							}
						}
						id := fmt.Sprintf("%v", c["id"])
						inReplyTo := fmt.Sprintf("%v", c["in_reply_to"])
						if inReplyTo == "<nil>" {
							inReplyTo = ""
						}
						dv.commentList = append(dv.commentList, commentEntry{
							filename:  filename,
							author:    author,
							message:   fmt.Sprintf("%v", c["message"]),
							id:        id,
							inReplyTo: inReplyTo,
						})
					}
				}
			}
		}
		dv.selectedComment = 0
		return dv, nil

	case openReplyEditorMsg:
		return dv, tea.ExecProcess(msg.cmd, func(err error) tea.Msg {
			return replyEditorFinishedMsg{err: err, replyFile: msg.replyFile, commentID: msg.commentID, filename: msg.filename}
		})

	case replyEditorFinishedMsg:
		return dv, dv.submitReplyComment(msg.replyFile, msg.commentID, msg.filename)

	case openEditorMsg:
		editor := "nvim"
		if _, err := exec.LookPath("nvim"); err != nil {
			editor = "vim"
		}
		initCmd := fmt.Sprintf(
			"let g:gerrit_comment_key='%s' | let g:gerrit_comment_file='%s' | source %s",
			msg.commentKey, msg.commentFile, msg.scriptPath,
		)
		cmd := exec.Command(editor, "-R", "-c", initCmd, msg.diffPath)
		commentFile := msg.commentFile
		return dv, tea.ExecProcess(cmd, func(err error) tea.Msg {
			os.Remove(msg.diffPath)
			os.Remove(msg.scriptPath)
			return editorFinishedMsg{err: err, commentFile: commentFile}
		})

	case editorFinishedMsg:
		if msg.commentFile != "" {
			return dv, dv.submitDraftComments(msg.commentFile)
		}
		return dv, nil

	case errMsg:
		dv.loading = false
		return dv, nil

	case actionResultMsg:
		// Handle action result (could show in status bar)
		// For now, just continue
		return dv, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, dv.keys.Back):
			// Return to list view
			return dv, func() tea.Msg {
				return switchToListMsg{}
			}

		// Focus switching
		case key.Matches(msg, dv.keys.FocusDown):
			// alt+j: from Summary/Review → Diff; from Diff → no-op
			if dv.activePane != PaneDiff {
				dv.prevPane = dv.activePane
				dv.activePane = PaneDiff
			}

		case key.Matches(msg, dv.keys.FocusUp):
			// alt+k: from Diff → return to prevPane
			if dv.activePane == PaneDiff {
				dv.activePane = dv.prevPane
			}

		case key.Matches(msg, dv.keys.FocusLeft):
			// alt+h: go left → Review → Summary
			if dv.activePane == PaneReview {
				dv.activePane = PaneSummary
			}

		case key.Matches(msg, dv.keys.FocusRight):
			// alt+l: go right → Summary → Review
			if dv.activePane == PaneSummary {
				dv.activePane = PaneReview
			}

		// Scrolling within active pane
		case key.Matches(msg, dv.keys.Up):
			switch dv.activePane {
			case PaneSummary:
				if dv.summaryScroll > 0 {
					dv.summaryScroll--
				}
			case PaneDiff:
				if dv.selectedFile > 0 {
					dv.selectedFile--
				}
			case PaneReview:
				if dv.selectedComment > 0 {
					dv.selectedComment--
				}
			}

		case key.Matches(msg, dv.keys.Down):
			paneHeight := dv.height/2 - 6
			switch dv.activePane {
			case PaneSummary:
				maxScroll := dv.summaryLineCount - paneHeight
				if maxScroll < 0 {
					maxScroll = 0
				}
				if dv.summaryScroll < maxScroll {
					dv.summaryScroll++
				}
			case PaneDiff:
				if dv.selectedFile < len(dv.fileList)-1 {
					dv.selectedFile++
				}
			case PaneReview:
				if dv.selectedComment < len(dv.commentList)-1 {
					dv.selectedComment++
				}
			}

		// Open file diff in editor or reply to comment
		case msg.Type == tea.KeyEnter:
			if dv.activePane == PaneDiff && len(dv.fileList) > 0 {
				filename := dv.fileList[dv.selectedFile]
				return dv, dv.openFileDiff(filename)
			}
			if dv.activePane == PaneReview && len(dv.commentList) > 0 {
				return dv, dv.replyToComment(dv.commentList[dv.selectedComment])
			}

		// Actions
		case key.Matches(msg, dv.keys.Fetch):
			// Fetch the change
			return dv, dv.fetchChange()

		case key.Matches(msg, dv.keys.CherryPick):
			// Cherry-pick the change
			return dv, dv.cherryPickChange()

		case key.Matches(msg, dv.keys.ReviewPlus1):
			return dv, dv.submitReview("Code-Review", 1)

		case key.Matches(msg, dv.keys.ReviewPlus2):
			return dv, dv.submitReview("Code-Review", 2)

		case key.Matches(msg, dv.keys.TestPlus1):
			return dv, dv.submitReview("Verified", 1)
		}
	}

	return dv, nil
}

// Styles for panes
var (
	activePaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("170")).
			Padding(1, 2)

	inactivePaneStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240")).
				Padding(1, 2)
)

// renderSummaryPane renders the summary pane
func (dv *DetailView) renderSummaryPane() string {
	if dv.loading {
		return "Loading..."
	}

	var lines []string

	// Subject
	subject := fmt.Sprintf("%v", dv.change["subject"])
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(subject))
	lines = append(lines, "")

	// Owner
	owner := "Unknown"
	if ownerData, ok := dv.change["owner"].(map[string]interface{}); ok {
		if name, ok := ownerData["name"].(string); ok {
			owner = name
		}
	}
	lines = append(lines, fmt.Sprintf("Owner: %s", owner))

	// Status
	lines = append(lines, fmt.Sprintf("Status: %v", dv.change["status"]))

	// Labels (Code-Review, Verified, etc.)
	if labels, ok := dv.change["labels"].(map[string]interface{}); ok {
		lines = append(lines, "")
		lines = append(lines, "Labels:")
		labelNames := make([]string, 0, len(labels))
		for label := range labels {
			labelNames = append(labelNames, label)
		}
		sort.Strings(labelNames)
		for _, label := range labelNames {
			data := labels[label]
			if labelData, ok := data.(map[string]interface{}); ok {
				if approved, ok := labelData["approved"].(map[string]interface{}); ok {
					name := approved["name"]
					lines = append(lines, fmt.Sprintf("  %s: ✓ %v", label, name))
				} else if rejected, ok := labelData["rejected"].(map[string]interface{}); ok {
					name := rejected["name"]
					lines = append(lines, fmt.Sprintf("  %s: ✗ %v", label, name))
				}
			}
		}
	}

	// Apply scroll offset (read-only, don't modify scroll variables)
	paneHeight := dv.height/2 - 6

	// Wrap lines then scroll
	wrappedLines := wrapLines(lines, dv.width/2-10)
	dv.summaryLineCount = len(wrappedLines)

	maxScroll := 0
	if len(wrappedLines) > paneHeight {
		maxScroll = len(wrappedLines) - paneHeight
	}

	scroll := dv.summaryScroll
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}

	visibleLines := wrappedLines
	if len(wrappedLines) > paneHeight {
		end := scroll + paneHeight
		if end > len(wrappedLines) {
			end = len(wrappedLines)
		}
		visibleLines = wrappedLines[scroll:end]
	}

	for len(visibleLines) < paneHeight {
		visibleLines = append(visibleLines, "")
	}

	content := lipgloss.JoinVertical(lipgloss.Left, visibleLines...)

	style := inactivePaneStyle
	if dv.activePane == PaneSummary {
		style = activePaneStyle
	}

	paneWidth := dv.width/2 - 4
	paneHeight = dv.height/2 - 6

	return style.Width(paneWidth).Height(paneHeight).Render(content)
}
// renderDiffPane renders the diff/files pane
func (dv *DetailView) renderDiffPane() string {
	if dv.loading {
		return "Loading..."
	}

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Files Changed:"))
	lines = append(lines, "")

	for i, filename := range dv.fileList {
		fileData := dv.files[filename]

		// Extract lines added/deleted
		linesInserted := 0
		linesDeleted := 0
		if data, ok := fileData.(map[string]interface{}); ok {
			if inserted, ok := data["lines_inserted"].(float64); ok {
				linesInserted = int(inserted)
			}
			if deleted, ok := data["lines_deleted"].(float64); ok {
				linesDeleted = int(deleted)
			}
		}

		prefix := "  "
		if dv.activePane == PaneDiff && i == dv.selectedFile {
			prefix = "▸ "
		}
		line := fmt.Sprintf("%s%s (+%d -%d)", prefix, filename, linesInserted, linesDeleted)
		lines = append(lines, line)
	}

	if len(dv.fileList) == 0 {
		lines = append(lines, "No files changed")
	}

	// Apply scroll offset - auto-scroll to keep selected file visible
	paneHeight := dv.height/2 - 6

	// Calculate scroll to keep selectedFile visible (selectedFile is at lines index selectedFile+2)
	selectedLineIdx := dv.selectedFile + 2
	scroll := dv.diffScroll
	if dv.activePane == PaneDiff {
		if selectedLineIdx < scroll {
			scroll = selectedLineIdx
		} else if selectedLineIdx >= scroll+paneHeight {
			scroll = selectedLineIdx - paneHeight + 1
		}
	}

	// Clamp scroll
	maxScroll := 0
	if len(lines) > paneHeight {
		maxScroll = len(lines) - paneHeight
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}

	visibleLines := lines
	if len(lines) > paneHeight {
		end := scroll + paneHeight
		if end > len(lines) {
			end = len(lines)
		}
		visibleLines = lines[scroll:end]
	}
	
	// Pad with empty lines to ensure consistent height
	for len(visibleLines) < paneHeight {
		visibleLines = append(visibleLines, "")
	}

	// Truncate each line to prevent wrapping
	truncatedLines := make([]string, len(visibleLines))
	for i, line := range visibleLines {
		wrapped := wrapLine(line, dv.width-8)
		truncatedLines[i] = wrapped[0]
	}

	content := lipgloss.JoinVertical(lipgloss.Left, truncatedLines...)

	style := inactivePaneStyle
	if dv.activePane == PaneDiff {
		style = activePaneStyle
	}

	paneWidth := dv.width - 4 // Full width
	paneHeight = dv.height/2 - 6

	return style.Width(paneWidth).Height(paneHeight).Render(content)
}

// renderReviewPane renders the review pane
func (dv *DetailView) renderReviewPane() string {
	if dv.loading {
		return "Loading..."
	}

	var lines []string
	// lineToComment maps wrapped line index → commentList index (for scroll tracking)
	lineToComment := make(map[int]int)

	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("Comments:"))
	lines = append(lines, "")

	if len(dv.commentList) == 0 {
		lines = append(lines, "No comments yet")
	} else {
		for i, c := range dv.commentList {
			// Record which line this comment starts at
			lineToComment[len(lines)] = i

			prefix := "  "
			if dv.activePane == PaneReview && i == dv.selectedComment {
				prefix = "▸ "
			}
			header := fmt.Sprintf("%s[%s] %s:", prefix, c.filename, c.author)
			lines = append(lines, header)
			msgLines := strings.Split(strings.ReplaceAll(c.message, "\r\n", "\n"), "\n")
			for _, msgLine := range msgLines {
				lines = append(lines, fmt.Sprintf("    %s", msgLine))
			}
			lines = append(lines, "")
		}
	}

	paneHeight := dv.height/2 - 6

	// Wrap lines to prevent overflow (supports CJK)
	wrappedLines := wrapLines(lines, dv.width/2-10)
	dv.reviewLineCount = len(wrappedLines)

	// Auto-scroll to keep selected comment visible
	// Find the wrapped line index of the selected comment
	selectedLineIdx := 0
	for rawIdx, commentIdx := range lineToComment {
		if commentIdx == dv.selectedComment {
			// Count wrapped lines before rawIdx
			wl := 0
			for li, l := range lines {
				if li >= rawIdx {
					break
				}
				wl += len(wrapLine(l, dv.width/2-10))
			}
			selectedLineIdx = wl
			break
		}
	}

	scroll := dv.reviewScroll
	if selectedLineIdx < scroll {
		scroll = selectedLineIdx
	} else if selectedLineIdx >= scroll+paneHeight {
		scroll = selectedLineIdx - paneHeight + 1
	}

	maxScroll := 0
	if len(wrappedLines) > paneHeight {
		maxScroll = len(wrappedLines) - paneHeight
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	dv.reviewScroll = scroll

	visibleLines := wrappedLines
	if len(wrappedLines) > paneHeight {
		end := scroll + paneHeight
		if end > len(wrappedLines) {
			end = len(wrappedLines)
		}
		visibleLines = wrappedLines[scroll:end]
	}

	for len(visibleLines) < paneHeight {
		visibleLines = append(visibleLines, "")
	}

	content := lipgloss.JoinVertical(lipgloss.Left, visibleLines...)

	style := inactivePaneStyle
	if dv.activePane == PaneReview {
		style = activePaneStyle
	}

	paneWidth := dv.width/2 - 4 // Same width as Summary
	paneHeight = dv.height/2 - 6

	return style.Width(paneWidth).Height(paneHeight).Render(content)
}

// View implements tea.Model
func (dv *DetailView) View() string {
	if dv.width == 0 {
		return "Initializing..."
	}

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("170")).
		Render(fmt.Sprintf("Change Details - %s", dv.changeID))

	// Help
	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render("alt+j/k: pane↕ | alt+h/l: pane↔ | k/j: scroll | f: fetch | C: cherry-pick | alt+c/C: CR+1/+2 | alt+t: TB+1 | q: back")

	// Top row: Summary and Review side by side
	topRow := lipgloss.JoinHorizontal(
		lipgloss.Top,
		dv.renderSummaryPane(),
		dv.renderReviewPane(),
	)

	// Bottom row: Diff pane (file changed)
	bottomRow := dv.renderDiffPane()

	// Combine all parts
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		topRow,
		bottomRow,
	)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		content,
		"",
		help,
	)
}

// fetchChange fetches the change using git
func (dv *DetailView) fetchChange() tea.Cmd {
	return func() tea.Msg {
		// Build refs path
		refsPath := fmt.Sprintf("refs/changes/%s/%s/1",
			dv.changeID[len(dv.changeID)-2:],
			dv.changeID)

		// Build remote URL
		remoteURL := fmt.Sprintf("ssh://%s@%s:%d/%s",
			dv.cfg.User,
			dv.cfg.Server,
			dv.cfg.Port,
			getProjectFromChange(dv.change))

		// Execute git fetch
		cmd := exec.Command("git", "fetch", remoteURL, refsPath)
		if err := cmd.Run(); err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("Fetch failed: %v", err)}
		}

		return actionResultMsg{success: true, message: "Change fetched successfully! Use 'git checkout FETCH_HEAD'"}
	}
}

// cherryPickChange cherry-picks the change
func (dv *DetailView) cherryPickChange() tea.Cmd {
	return func() tea.Msg {
		// First fetch
		refsPath := fmt.Sprintf("refs/changes/%s/%s/1",
			dv.changeID[len(dv.changeID)-2:],
			dv.changeID)

		remoteURL := fmt.Sprintf("ssh://%s@%s:%d/%s",
			dv.cfg.User,
			dv.cfg.Server,
			dv.cfg.Port,
			getProjectFromChange(dv.change))

		// Fetch
		fetchCmd := exec.Command("git", "fetch", remoteURL, refsPath)
		if err := fetchCmd.Run(); err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("Fetch failed: %v", err)}
		}

		// Cherry-pick
		cherryCmd := exec.Command("git", "cherry-pick", "FETCH_HEAD")
		if err := cherryCmd.Run(); err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("Cherry-pick failed: %v", err)}
		}

		return actionResultMsg{success: true, message: "Change cherry-picked successfully!"}
	}
}

// openFileDiff fetches the diff for a file and opens it in an editor
func (dv *DetailView) openFileDiff(filename string) tea.Cmd {
	return func() tea.Msg {
		diff, err := dv.client.GetFileDiff(dv.changeID, "current", filename)
		if err != nil {
			return errMsg{err}
		}

		// Write diff to temp file
		diffFile, err := os.CreateTemp("", "gerrit-diff-*.diff")
		if err != nil {
			return errMsg{err}
		}
		diffFile.WriteString(diff)
		diffFile.Close()

		// Write vim script to temp file
		scriptFile, err := os.CreateTemp("", "gerrit-comment-*.vim")
		if err != nil {
			os.Remove(diffFile.Name())
			return errMsg{err}
		}
		scriptFile.WriteString(gerritCommentVim)
		scriptFile.Close()

		// Comment output file: per change+filename
		safeFilename := strings.ReplaceAll(filename, "/", "_")
		commentFile := fmt.Sprintf("/tmp/gerrit-%s-%s-comments.json", dv.changeID, safeFilename)

		// Get comment keybinding (first key in the binding)
		commentKey := "gc"
		if keys := dv.keys.InlineComment.Keys(); len(keys) > 0 {
			commentKey = keys[0]
		}

		return openEditorMsg{
			diffPath:    diffFile.Name(),
			scriptPath:  scriptFile.Name(),
			commentFile: commentFile,
			commentKey:  commentKey,
		}
	}
}

// submitDraftComments reads the comment JSON file and submits each as a Gerrit draft
func (dv *DetailView) submitDraftComments(commentFile string) tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(commentFile)
		if err != nil || len(data) == 0 {
			return nil
		}

		var comments []map[string]interface{}
		if err := json.Unmarshal(data, &comments); err != nil || len(comments) == 0 {
			return nil
		}

		for _, c := range comments {
			draft := map[string]interface{}{
				"path":       c["path"],
				"side":       c["side"],
				"message":    c["message"],
				"unresolved": true,
			}
			// Convert float64 line to int
			if lineFloat, ok := c["line"].(float64); ok && lineFloat > 0 {
				draft["line"] = int(lineFloat)
			}
			if err := dv.client.CreateDraftComment(dv.changeID, "current", draft); err != nil {
				return actionResultMsg{success: false, message: fmt.Sprintf("Failed to submit comment: %v", err)}
			}
		}

		// Publish all drafts with empty reply
		if err := dv.client.PublishDraftComments(dv.changeID, "current"); err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("Failed to publish comments: %v", err)}
		}

		os.Remove(commentFile)
		return actionResultMsg{success: true, message: fmt.Sprintf("%d comment(s) published", len(comments))}
	}
}

// replyToComment opens an editor to reply to a specific comment
func (dv *DetailView) replyToComment(c commentEntry) tea.Cmd {
	return func() tea.Msg {
		// Write a temp file with context for the reply
		tmpFile, err := os.CreateTemp("", "gerrit-reply-*.txt")
		if err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("Failed to create temp file: %v", err)}
		}
		// Write context header as comments
		context := fmt.Sprintf("# Reply to comment by %s in %s\n# Original: %s\n# (Lines starting with # are ignored)\n\n",
			c.author, c.filename, strings.ReplaceAll(c.message, "\n", "\n# "))
		tmpFile.WriteString(context)
		tmpFile.Close()

		editor := "nvim"
		if _, err := exec.LookPath("nvim"); err != nil {
			editor = "vim"
		}
		cmd := exec.Command(editor, tmpFile.Name())
		replyFile := tmpFile.Name()
		commentID := c.id
		filename := c.filename
		return openReplyEditorMsg{cmd: cmd, replyFile: replyFile, commentID: commentID, filename: filename}
	}
}

// submitReplyComment reads the reply file and posts it to Gerrit
func (dv *DetailView) submitReplyComment(replyFile, commentID, filename string) tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(replyFile)
		os.Remove(replyFile)
		if err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("Failed to read reply: %v", err)}
		}
		// Strip comment lines (starting with #) and trim
		var msgLines []string
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") {
				msgLines = append(msgLines, line)
			}
		}
		message := strings.TrimSpace(strings.Join(msgLines, "\n"))
		if message == "" {
			return actionResultMsg{success: false, message: "Reply cancelled (empty message)"}
		}
		if err := dv.client.ReplyComment(dv.changeID, "current", filename, commentID, message); err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("Failed to post reply: %v", err)}
		}
		return actionResultMsg{success: true, message: "Reply posted"}
	}
}

// submitReview submits a label vote (e.g. Code-Review +1, Verified +2)
func (dv *DetailView) submitReview(label string, value int) tea.Cmd {
	return func() tea.Msg {
		if err := dv.client.SubmitReview(dv.changeID, "current", label, value); err != nil {
			return actionResultMsg{success: false, message: fmt.Sprintf("Review failed: %v", err)}
		}
		return actionResultMsg{success: true, message: fmt.Sprintf("%s +%d submitted", label, value)}
	}
}

// getProjectFromChange extracts project name from change data
func getProjectFromChange(change map[string]interface{}) string {
	if project, ok := change["project"].(string); ok {
		return project
	}
	return ""
}

// actionResultMsg represents the result of an action
type actionResultMsg struct {
	success bool
	message string
}

// openEditorMsg triggers opening an external editor
type openEditorMsg struct {
	diffPath    string
	scriptPath  string
	commentFile string
	commentKey  string
}

// commentEntry represents a single comment for selection
type commentEntry struct {
	filename  string
	author    string
	message   string
	id        string
	inReplyTo string
}

// openReplyEditorMsg triggers opening an editor for replying to a comment
type openReplyEditorMsg struct {
	cmd       *exec.Cmd
	replyFile string
	commentID string
	filename  string
}

// replyEditorFinishedMsg is sent when the reply editor exits
type replyEditorFinishedMsg struct {
	err       error
	replyFile string
	commentID string
	filename  string
}

// editorFinishedMsg is sent when the editor exits
type editorFinishedMsg struct {
	err         error
	commentFile string
}

// runeWidth returns the display width of a rune (CJK = 2, others = 1)
func runeWidth(r rune) int {
	if r >= 0x1100 &&
		(r <= 0x115f || r == 0x2329 || r == 0x232a ||
			(r >= 0x2e80 && r <= 0x303e) ||
			(r >= 0x3040 && r <= 0x33bf) ||
			(r >= 0x3400 && r <= 0x4dbf) ||
			(r >= 0x4e00 && r <= 0xa4cf) ||
			(r >= 0xa960 && r <= 0xa97c) ||
			(r >= 0xac00 && r <= 0xd7a3) ||
			(r >= 0xf900 && r <= 0xfaff) ||
			(r >= 0xfe10 && r <= 0xfe6f) ||
			(r >= 0xff01 && r <= 0xff60) ||
			(r >= 0xffe0 && r <= 0xffe6) ||
			(r >= 0x1f300 && r <= 0x1f9ff) ||
			(r >= 0x20000 && r <= 0x2fffd) ||
			(r >= 0x30000 && r <= 0x3fffd)) {
		return 2
	}
	return 1
}

// stringWidth returns the display width of a string
func stringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// wrapLine wraps a single line by display width (CJK chars count as 2)
func wrapLine(line string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{line}
	}
	if stringWidth(line) <= maxWidth {
		return []string{line}
	}
	var result []string
	runes := []rune(line)
	for len(runes) > 0 {
		w := 0
		i := 0
		for i < len(runes) {
			rw := runeWidth(runes[i])
			if w+rw > maxWidth {
				break
			}
			w += rw
			i++
		}
		if i == 0 {
			i = 1 // at least one rune per line
		}
		result = append(result, string(runes[:i]))
		runes = runes[i:]
	}
	return result
}

// wrapLines wraps a slice of lines, expanding each into multiple lines if needed
func wrapLines(lines []string, maxWidth int) []string {
	var result []string
	for _, line := range lines {
		result = append(result, wrapLine(line, maxWidth)...)
	}
	return result
}
