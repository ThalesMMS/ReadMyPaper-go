package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ThalesMMS/ReadMyPaper-go/internal/config"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/domain"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/jobs"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/tts"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/util"
)

const noPDFSelected = "No PDF selected"

// Desktop builds the native Fyne interface and projects Store state into a
// job list/detail view. Processing remains outside the UI thread.
type Desktop struct {
	application fyne.App
	window      fyne.Window
	settings    config.Settings
	store       *jobs.Store
	manager     *jobs.Manager
	catalog     *tts.Catalog
	player      AudioPlayer

	tabs       *container.AppTabs
	newJobTab  *container.TabItem
	jobsTab    *container.TabItem
	jobList    *widget.List
	jobsMu     sync.RWMutex
	jobs       []domain.JobState
	selectedID string

	selectedPDF string
	fileLabel   *widget.Label
	language    *widget.Select
	voice       *widget.Select
	engine      *widget.Select
	rate        *widget.Slider
	rateLabel   *widget.Label
	removeCites *widget.Check
	dropRefs    *widget.Check
	dropAcks    *widget.Check
	dropApps    *widget.Check
	keepHeads   *widget.Check
	useLLM      *widget.Check
	llmURL      *widget.Entry
	llmModel    *widget.Entry
	llmAPIKey   *widget.Entry
	llmFields   *fyne.Container
	process     *widget.Button
	formStatus  *widget.Label

	voiceDisplayToKey map[string]string

	detailTitle    *widget.Label
	detailID       *widget.Label
	detailStatus   *widget.Label
	detailStep     *widget.Label
	detailPercent  *widget.Label
	detailProgress *widget.ProgressBar
	detailError    *widget.Label
	playButton     *widget.Button
	stopButton     *widget.Button
	saveAudio      *widget.Button
	saveText       *widget.Button
	savePDF        *widget.Button
	deleteJob      *widget.Button
	preview        *widget.Entry
	statsLabel     *widget.Label

	stopPolling chan struct{}
	closeOnce   sync.Once
}

func NewDesktop(application fyne.App, settings config.Settings, store *jobs.Store, manager *jobs.Manager, catalog *tts.Catalog) *Desktop {
	window := application.NewWindow("ReadMyPaper")
	desktop := &Desktop{
		application: application, window: window, settings: settings,
		store: store, manager: manager, catalog: catalog,
		voiceDisplayToKey: make(map[string]string), stopPolling: make(chan struct{}),
	}
	desktop.build()
	return desktop
}

func (d *Desktop) Window() fyne.Window { return d.window }

func (d *Desktop) build() {
	d.newJobTab = container.NewTabItemWithIcon("New job", theme.ContentAddIcon(), d.buildNewJobView())
	d.jobsTab = container.NewTabItemWithIcon("Jobs", theme.ListIcon(), d.buildJobsView())
	d.tabs = container.NewAppTabs(d.newJobTab, d.jobsTab)
	d.tabs.SetTabLocation(container.TabLocationTop)

	headerTitle := widget.NewLabelWithStyle("ReadMyPaper", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	headerTitle.TextStyle = fyne.TextStyle{Bold: true}
	headerSubtitle := widget.NewLabel("Local PDF → cleaned scientific text → spoken reading")
	header := container.NewVBox(headerTitle, headerSubtitle)

	d.window.SetContent(container.NewBorder(container.NewPadded(header), nil, nil, nil, d.tabs))
	d.window.Resize(fyne.NewSize(1080, 820))
	d.window.CenterOnScreen()
	d.window.SetCloseIntercept(func() {
		d.closeOnce.Do(func() {
			close(d.stopPolling)
			d.player.Stop()
			if d.manager != nil {
				d.manager.Shutdown()
			}
			// Remove the intercept before closing to avoid invoking it recursively.
			d.window.SetCloseIntercept(nil)
			d.window.Close()
		})
	})
	d.refreshJobs()
	go d.pollStore()
}

func (d *Desktop) buildNewJobView() fyne.CanvasObject {
	intro := widget.NewCard(
		"ReadMyPaper",
		"Private, local conversion of scientific papers to spoken audio",
		wrapLabel("Choose a scientific PDF. The app repairs common multi-column reading order, removes layout noise and end matter, then generates a WAV reading in English or Brazilian Portuguese. Voice models are downloaded only when first used."),
	)

	d.fileLabel = wrapLabel(noPDFSelected)
	chooseFile := widget.NewButtonWithIcon("Choose PDF", theme.FolderOpenIcon(), d.choosePDF)
	fileRow := container.NewBorder(nil, nil, chooseFile, nil, container.NewPadded(d.fileLabel))

	d.language = widget.NewSelect([]string{"Auto-detect", "English", "Português (Brasil)"}, nil)
	d.language.SetSelected("Auto-detect")
	d.engine = widget.NewSelect([]string{"Piper — Fast", "Kokoro — Quality"}, func(_ string) {
		d.refreshVoiceOptions()
	})
	d.engine.SetSelected("Piper — Fast")
	d.voice = widget.NewSelect(nil, nil)
	d.refreshVoiceOptions()

	d.rate = widget.NewSlider(d.settings.SpeechRateMin, d.settings.SpeechRateMax)
	d.rate.Step = 0.05
	d.rate.Value = 1
	d.rateLabel = widget.NewLabel("1.00×")
	d.rateLabel.Alignment = fyne.TextAlignTrailing
	d.rate.OnChanged = func(value float64) { d.rateLabel.SetText(fmt.Sprintf("%.2f×", value)) }
	rateRow := container.NewBorder(nil, nil, nil, d.rateLabel, d.rate)

	fields := container.NewGridWithColumns(2,
		field("Language", d.language), field("Voice", d.voice),
		field("TTS engine", d.engine), field("Speech rate", rateRow),
	)

	d.removeCites = widget.NewCheck("Remove numeric citations such as [12] or (3,4)", nil)
	d.dropRefs = widget.NewCheck("Drop references / bibliography sections", nil)
	d.dropAcks = widget.NewCheck("Drop acknowledgements / funding / conflicts sections", nil)
	d.dropApps = widget.NewCheck("Drop appendices / supplementary sections", nil)
	d.keepHeads = widget.NewCheck("Keep titles and section headings in spoken text", nil)
	for _, check := range []*widget.Check{d.removeCites, d.dropRefs, d.dropAcks, d.dropApps, d.keepHeads} {
		check.SetChecked(true)
	}
	d.useLLM = widget.NewCheck("Use a local OpenAI-compatible LLM to review blocks (experimental)", func(enabled bool) {
		// SetChecked may invoke this callback before llmFields is assigned.
		if d.llmFields == nil {
			return
		}
		if enabled {
			d.llmFields.Show()
		} else {
			d.llmFields.Hide()
		}
	})
	d.useLLM.SetChecked(d.settings.LLMEnabled)
	d.llmURL = widget.NewEntry()
	d.llmURL.SetPlaceHolder("http://127.0.0.1:11434/v1")
	d.llmURL.SetText(d.settings.LLMBaseURL)
	d.llmModel = widget.NewEntry()
	d.llmModel.SetPlaceHolder("Optional model name")
	d.llmModel.SetText(d.settings.LLMModel)
	d.llmAPIKey = widget.NewPasswordEntry()
	d.llmAPIKey.SetPlaceHolder("Optional bearer token")
	d.llmAPIKey.SetText(d.settings.LLMAPIKey)
	llmHelp := wrapLabel("The endpoint must expose /chat/completions. Failed LLM batches are kept unchanged.")
	d.llmFields = container.NewVBox(field("LLM base URL", d.llmURL), llmHelp, field("Model", d.llmModel), field("API key", d.llmAPIKey))
	if !d.settings.LLMEnabled {
		d.llmFields.Hide()
	}
	cleanup := widget.NewCard("Cleanup rules", "", container.NewVBox(
		d.removeCites, d.dropRefs, d.dropAcks, d.dropApps, d.keepHeads, d.useLLM, d.llmFields,
	))

	d.process = widget.NewButtonWithIcon("Process PDF", theme.MediaPlayIcon(), d.submitJob)
	d.process.Importance = widget.HighImportance
	d.formStatus = wrapLabel("")
	form := widget.NewCard("New job", "", container.NewVBox(
		field("PDF", fileRow), fields, cleanup, d.formStatus, d.process,
	))

	how := widget.NewCard("How it works", "", wrapLabel(
		"1. Text and coordinates are extracted locally from the PDF.\n"+
			"2. Multi-column reading order is repaired page by page.\n"+
			"3. Spatial and deterministic rules remove figures, tables, citations and end matter.\n"+
			"4. Scientific notation is expanded for natural listening.\n"+
			"5. Piper (fast) or Kokoro (quality) synthesizes a local WAV file.",
	))
	content := container.NewVBox(intro, form, how)
	return container.NewVScroll(container.NewPadded(content))
}

func (d *Desktop) buildJobsView() fyne.CanvasObject {
	d.jobList = widget.NewList(
		func() int {
			d.jobsMu.RLock()
			defer d.jobsMu.RUnlock()
			return len(d.jobs)
		},
		func() fyne.CanvasObject {
			title := widget.NewLabelWithStyle("document.pdf", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			status := widget.NewLabel("Queued")
			return container.NewBorder(nil, nil, nil, status, title)
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			d.jobsMu.RLock()
			defer d.jobsMu.RUnlock()
			if id < 0 || id >= len(d.jobs) {
				return
			}
			job := d.jobs[id]
			row := object.(*fyne.Container)
			title := row.Objects[0].(*widget.Label)
			status := row.Objects[1].(*widget.Label)
			title.SetText(job.Filename)
			status.SetText(statusText(job))
		},
	)
	d.jobList.OnSelected = func(id widget.ListItemID) {
		d.jobsMu.RLock()
		if id >= 0 && id < len(d.jobs) {
			d.selectedID = d.jobs[id].JobID
		}
		d.jobsMu.RUnlock()
		d.refreshDetail()
	}

	d.detailTitle = widget.NewLabelWithStyle("Select a job", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	d.detailID = wrapLabel("")
	d.detailStatus = widget.NewLabel("")
	d.detailStatus.Alignment = fyne.TextAlignTrailing
	d.detailStep = widget.NewLabel("")
	d.detailPercent = widget.NewLabel("0%")
	d.detailPercent.Alignment = fyne.TextAlignTrailing
	d.detailProgress = widget.NewProgressBar()
	d.detailError = wrapLabel("")
	d.detailError.Importance = widget.DangerImportance

	d.saveAudio = widget.NewButtonWithIcon("Save WAV", theme.DocumentSaveIcon(), func() { d.saveCurrentArtifact("audio") })
	d.saveText = widget.NewButtonWithIcon("Save cleaned text", theme.DocumentSaveIcon(), func() { d.saveCurrentArtifact("text") })
	d.savePDF = widget.NewButtonWithIcon("Save original PDF", theme.DocumentSaveIcon(), func() { d.saveCurrentArtifact("pdf") })
	d.deleteJob = widget.NewButtonWithIcon("Delete job", theme.DeleteIcon(), d.confirmDeleteCurrent)
	d.playButton = widget.NewButtonWithIcon("Play", theme.MediaPlayIcon(), d.playCurrent)
	d.stopButton = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), d.player.Stop)

	d.preview = widget.NewMultiLineEntry()
	d.preview.Wrapping = fyne.TextWrapWord
	d.preview.SetMinRowsVisible(14)
	d.statsLabel = wrapLabel("No statistics available.")

	progressHeader := container.NewBorder(nil, nil, d.detailStep, d.detailPercent)
	actions := container.NewHBox(d.saveAudio, d.saveText, d.savePDF, d.deleteJob)
	audioActions := container.NewHBox(d.playButton, d.stopButton)
	jobCard := widget.NewCard("", "", container.NewVBox(
		container.NewBorder(nil, nil, d.detailTitle, d.detailStatus),
		d.detailID, progressHeader, d.detailProgress, d.detailError, actions,
	))
	audioCard := widget.NewCard("Audio", "Playback uses the operating system's local WAV player.", audioActions)
	previewCard := widget.NewCard("Cleaned text preview", "", d.preview)
	statsCard := widget.NewCard("Processing statistics", "", d.statsLabel)
	detail := container.NewVScroll(container.NewPadded(container.NewVBox(jobCard, audioCard, previewCard, statsCard)))

	listCard := widget.NewCard("Jobs", "Saved jobs are restored on startup.", d.jobList)
	listCard.Resize(fyne.NewSize(320, 600))
	split := container.NewHSplit(container.NewPadded(listCard), detail)
	split.Offset = 0.31
	return split
}

func (d *Desktop) choosePDF() {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		if reader == nil {
			return
		}
		path := reader.URI().Path()
		_ = reader.Close()
		if path == "" {
			dialog.ShowError(fmt.Errorf("the selected file has no local filesystem path"), d.window)
			return
		}
		d.selectedPDF = path
		d.fileLabel.SetText(path)
		d.formStatus.SetText("")
	}, d.window)
	picker.SetFilter(storage.NewExtensionFileFilter([]string{".pdf", ".PDF"}))
	picker.Show()
}

func (d *Desktop) refreshVoiceOptions() {
	if d.voice == nil || d.catalog == nil {
		return
	}
	engine := selectedEngine(d.engine)
	currentKey := d.voiceDisplayToKey[d.voice.Selected]
	d.voiceDisplayToKey = map[string]string{"Auto-select from detected language": "auto"}
	options := []string{"Auto-select from detected language"}
	voices := d.catalog.List()
	for _, voice := range voices {
		if voice.Engine != engine {
			continue
		}
		options = append(options, voice.DisplayName)
		d.voiceDisplayToKey[voice.DisplayName] = voice.Key
	}
	d.voice.Options = options
	selection := "Auto-select from detected language"
	for display, key := range d.voiceDisplayToKey {
		if key == currentKey {
			selection = display
			break
		}
	}
	d.voice.SetSelected(selection)
	d.voice.Refresh()
}

func (d *Desktop) submitJob() {
	pdfPath := strings.TrimSpace(d.selectedPDF)
	if pdfPath == "" {
		dialog.ShowError(fmt.Errorf("choose a PDF first"), d.window)
		return
	}
	info, err := os.Stat(pdfPath)
	if err != nil {
		dialog.ShowError(err, d.window)
		return
	}
	if info.Size() > d.settings.MaxUploadBytes {
		dialog.ShowError(fmt.Errorf("PDF is %.1f MB; configured limit is %.1f MB", float64(info.Size())/(1024*1024), float64(d.settings.MaxUploadBytes)/(1024*1024)), d.window)
		return
	}
	if err := util.IsPDF(pdfPath); err != nil {
		dialog.ShowError(err, d.window)
		return
	}
	options := domain.DefaultProcessingOptions()
	options.Language = selectedLanguage(d.language.Selected)
	options.VoiceKey = d.voiceDisplayToKey[d.voice.Selected]
	if options.VoiceKey == "" {
		options.VoiceKey = "auto"
	}
	options.SpeechRate = d.rate.Value
	options.RemoveNumericCitations = d.removeCites.Checked
	options.DropReferencesSection = d.dropRefs.Checked
	options.DropAcknowledgements = d.dropAcks.Checked
	options.DropAppendices = d.dropApps.Checked
	options.KeepHeadings = d.keepHeads.Checked
	options.TTSEngine = selectedEngine(d.engine)
	options.UseLLMCleaner = d.useLLM.Checked
	options.LLMModel = strings.TrimSpace(d.llmModel.Text)
	options.LLMAPIKey = strings.TrimSpace(d.llmAPIKey.Text)
	if options.UseLLMCleaner {
		normalized, err := util.NormalizeBaseURL(d.llmURL.Text)
		if err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		if normalized == "" {
			dialog.ShowError(fmt.Errorf("LLM base URL is required when the LLM cleaner is enabled"), d.window)
			return
		}
		options.LLMBaseURL = normalized
	}
	filename := filepath.Base(pdfPath)
	d.process.Disable()
	d.formStatus.SetText("Creating job…")
	go d.createAndStartJob(pdfPath, filename, options)
}

func (d *Desktop) createAndStartJob(sourcePath, filename string, options domain.ProcessingOptions) {
	job, err := d.store.Create(filename, options, d.settings.MaxPendingJobs)
	if err != nil {
		fyne.Do(func() {
			d.process.Enable()
			d.formStatus.SetText("")
			dialog.ShowError(err, d.window)
		})
		return
	}
	sourceDestination := filepath.Join(d.settings.UploadsDir(), job.JobID, "source.pdf")
	outputDir := filepath.Join(d.settings.OutputsDir(), job.JobID)
	if err := util.CopyFile(sourcePath, sourceDestination); err != nil {
		_, _ = d.store.Update(job.JobID, func(state *domain.JobState) {
			state.Status = domain.JobFailed
			state.Step = "Failed"
			state.Error = err.Error()
		})
		fyne.Do(func() {
			d.process.Enable()
			dialog.ShowError(err, d.window)
			d.refreshJobs()
		})
		return
	}
	job.Options.JobID = job.JobID
	job.Options.Filename = job.Filename
	job.Options.CreatedAt = job.CreatedAt.Format(time.RFC3339Nano)
	_, _ = d.store.Update(job.JobID, func(state *domain.JobState) { state.Options = job.Options })
	if err := d.manager.Start(job.JobID, sourceDestination, outputDir, job.Options); err != nil {
		_, _ = d.store.Update(job.JobID, func(state *domain.JobState) {
			state.Status = domain.JobFailed
			state.Step = "Failed"
			state.Error = err.Error()
		})
	}
	fyne.Do(func() {
		d.process.Enable()
		d.formStatus.SetText("Job queued. Progress is available in the Jobs tab.")
		d.selectedID = job.JobID
		d.tabs.Select(d.jobsTab)
		d.refreshJobs()
		d.selectJobByID(job.JobID)
	})
}

func (d *Desktop) pollStore() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	lastVersion := d.store.Version()
	for {
		select {
		case <-d.stopPolling:
			return
		case <-ticker.C:
			version := d.store.Version()
			if version == lastVersion {
				continue
			}
			lastVersion = version
			fyne.Do(d.refreshJobs)
		}
	}
}

func (d *Desktop) refreshJobs() {
	list := d.store.List()
	d.jobsMu.Lock()
	d.jobs = list
	d.jobsMu.Unlock()
	if d.jobList != nil {
		d.jobList.Refresh()
	}
	if d.selectedID == "" && len(list) > 0 {
		d.selectedID = list[0].JobID
	}
	d.refreshDetail()
}

func (d *Desktop) selectJobByID(jobID string) {
	d.jobsMu.RLock()
	defer d.jobsMu.RUnlock()
	for index, job := range d.jobs {
		if job.JobID == jobID {
			d.jobList.Select(index)
			return
		}
	}
}

func (d *Desktop) refreshDetail() {
	if d.detailTitle == nil {
		return
	}
	job, exists := d.store.Get(d.selectedID)
	if !exists {
		d.detailTitle.SetText("Select a job")
		d.detailID.SetText("")
		d.detailStatus.SetText("")
		d.detailStep.SetText("")
		d.detailPercent.SetText("0%")
		d.detailProgress.SetValue(0)
		d.detailError.SetText("")
		d.preview.SetText("")
		d.statsLabel.SetText("No statistics available.")
		d.setResultActions(false, false, false, false)
		return
	}
	d.detailTitle.SetText(job.Filename)
	d.detailID.SetText("Job ID: " + job.JobID)
	d.detailStatus.SetText(statusText(job))
	d.detailStep.SetText(job.Step)
	d.detailPercent.SetText(fmt.Sprintf("%.0f%%", job.Progress*100))
	d.detailProgress.SetValue(job.Progress)
	d.detailError.SetText(job.Error)
	hasAudio := fileExists(job.Result.AudioPath)
	hasText := fileExists(job.Result.CleanedTextPath)
	hasPDF := fileExists(job.Result.OriginalPDFPath)
	terminal := job.Status == domain.JobCompleted || job.Status == domain.JobFailed
	d.setResultActions(hasAudio, hasText, hasPDF, terminal)
	if hasText {
		d.preview.SetText(readPreview(job.Result.CleanedTextPath, 20000))
	} else if job.Status == domain.JobRunning || job.Status == domain.JobPending {
		d.preview.SetText("Cleaned text will appear here when processing reaches the text stage.")
	} else {
		d.preview.SetText("")
	}
	d.statsLabel.SetText(formatStats(job.Result.Stats, job.Result.DetectedLanguage, job.EngineUsed))
}

func (d *Desktop) setResultActions(audio, text, pdf, deletable bool) {
	setButtonEnabled(d.saveAudio, audio)
	setButtonEnabled(d.playButton, audio)
	setButtonEnabled(d.stopButton, audio)
	setButtonEnabled(d.saveText, text)
	setButtonEnabled(d.savePDF, pdf)
	setButtonEnabled(d.deleteJob, deletable)
}

func setButtonEnabled(button *widget.Button, enabled bool) {
	if button == nil {
		return
	}
	if enabled {
		button.Enable()
	} else {
		button.Disable()
	}
}

func (d *Desktop) currentJob() (domain.JobState, bool) { return d.store.Get(d.selectedID) }

func (d *Desktop) playCurrent() {
	job, exists := d.currentJob()
	if !exists || !fileExists(job.Result.AudioPath) {
		return
	}
	if err := d.player.Play(job.Result.AudioPath); err != nil {
		dialog.ShowError(err, d.window)
	}
}

func (d *Desktop) saveCurrentArtifact(kind string) {
	job, exists := d.currentJob()
	if !exists {
		return
	}
	source, name := "", ""
	switch kind {
	case "audio":
		source, name = job.Result.AudioPath, strings.TrimSuffix(job.Filename, filepath.Ext(job.Filename))+"-reading.wav"
	case "text":
		source, name = job.Result.CleanedTextPath, strings.TrimSuffix(job.Filename, filepath.Ext(job.Filename))+"-cleaned.txt"
	case "pdf":
		source, name = job.Result.OriginalPDFPath, job.Filename
	}
	if !fileExists(source) {
		return
	}
	saver := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		if writer == nil {
			return
		}
		go func() {
			copyErr := copyToWriter(source, writer)
			fyne.Do(func() {
				if copyErr != nil {
					dialog.ShowError(copyErr, d.window)
					return
				}
				dialog.ShowInformation("Saved", "File saved successfully.", d.window)
			})
		}()
	}, d.window)
	saver.SetFileName(name)
	saver.Show()
}

func copyToWriter(source string, writer fyne.URIWriteCloser) (err error) {
	defer func() {
		if closeErr := writer.Close(); err == nil {
			err = closeErr
		}
	}()
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}

func (d *Desktop) confirmDeleteCurrent() {
	job, exists := d.currentJob()
	if !exists {
		return
	}
	dialog.NewConfirm("Delete job", "Delete this job and its local PDF, text and audio files?", func(confirmed bool) {
		if !confirmed {
			return
		}
		d.player.Stop()
		if err := jobs.DeleteArtifacts(d.store, d.settings, job.JobID); err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		d.selectedID = ""
		d.refreshJobs()
	}, d.window).Show()
}

func field(label string, object fyne.CanvasObject) fyne.CanvasObject {
	title := widget.NewLabel(label)
	title.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewVBox(title, object)
}

func wrapLabel(text string) *widget.Label {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	return label
}

func selectedLanguage(display string) string {
	switch display {
	case "English":
		return "en"
	case "Português (Brasil)":
		return "pt-BR"
	default:
		return "auto"
	}
}

func selectedEngine(selectWidget *widget.Select) string {
	if selectWidget != nil && strings.HasPrefix(selectWidget.Selected, "Kokoro") {
		return "kokoro"
	}
	return "piper"
}

func statusText(job domain.JobState) string {
	switch job.Status {
	case domain.JobCompleted:
		return "Completed"
	case domain.JobFailed:
		return "Failed"
	case domain.JobRunning:
		return fmt.Sprintf("Running · %.0f%%", job.Progress*100)
	default:
		return "Queued"
	}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func readPreview(path string, maxBytes int64) string {
	if maxBytes <= 0 {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	data, _ := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if int64(len(data)) > maxBytes {
		data = data[:maxBytes]
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
		return string(data) + "\n\n[… preview truncated …]"
	}
	return strings.ToValidUTF8(string(data), "�")
}

func formatStats(stats *domain.CleaningStats, language, engine string) string {
	if stats == nil {
		if language == "" && engine == "" {
			return "No statistics available."
		}
		return fmt.Sprintf("Language: %s\nTTS engine: %s", fallback(language, "unknown"), fallback(engine, "unknown"))
	}
	lines := []string{
		fmt.Sprintf("Pages: %d", stats.Pages),
		fmt.Sprintf("Blocks: %d total · %d kept · %d dropped", stats.TotalBlocks, stats.KeptBlocks, stats.DroppedBlocks),
		fmt.Sprintf("Layout regions: %d · spatially dropped: %d", stats.LayoutRegionsFound, stats.LayoutFilterDropped),
		fmt.Sprintf("Language: %s · TTS engine: %s", fallback(language, stats.DetectedLanguage), fallback(engine, "unknown")),
	}
	if stats.LLMBlocksProcessed > 0 {
		lines = append(lines, fmt.Sprintf("LLM review: %d processed · %d dropped", stats.LLMBlocksProcessed, stats.LLMBlocksDropped))
	}
	if len(stats.DroppedByRule) > 0 {
		keys := make([]string, 0, len(stats.DroppedByRule))
		for key := range stats.DroppedByRule {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s=%d", key, stats.DroppedByRule[key]))
		}
		lines = append(lines, "Drop rules: "+strings.Join(parts, ", "))
	}
	return strings.Join(lines, "\n")
}

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}
