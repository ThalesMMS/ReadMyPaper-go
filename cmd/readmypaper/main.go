package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"

	desktopapp "github.com/ThalesMMS/ReadMyPaper-go/internal/app"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/config"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/jobs"
	"github.com/ThalesMMS/ReadMyPaper-go/internal/pipeline"
)

const version = "0.2.0"

//go:embed Icon.png
var iconBytes []byte

func main() {
	for _, argument := range os.Args[1:] {
		if argument == "--version" || argument == "-version" {
			fmt.Printf("readmypaper %s\n", version)
			return
		}
	}

	settings, err := config.Load()
	if err != nil {
		log.Fatalf("ReadMyPaper configuration error: %v", err)
	}
	log.Printf("ReadMyPaper %s", version)
	log.Printf("Data directory: %s", settings.DataDir)
	log.Printf("Cache directory: %s", settings.CacheDir)

	store := jobs.NewStore()
	for _, warning := range jobs.RestoreFromDisk(store, settings) {
		log.Printf("ReadMyPaper restore warning: %v", warning)
	}
	for _, warning := range jobs.CleanupExpired(store, settings, time.Now().UTC()) {
		log.Printf("ReadMyPaper retention warning: %v", warning)
	}

	processor := pipeline.New(settings)
	manager := jobs.NewManager(store, processor, settings.MaxWorkers)
	application := fyneapp.NewWithID("io.github.thalesmms.readmypaper")
	application.SetIcon(fyne.NewStaticResource("Icon.png", iconBytes))
	application.Settings().SetTheme(desktopapp.NewTheme())
	desktop := desktopapp.NewDesktop(application, settings, store, manager, processor.Catalog)
	desktop.Window().ShowAndRun()
}
