package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"

	"github.com/eric-carlsson/gnome-spotlight/api"
)

type Config struct {
	debug    bool
	dir      string
	preserve uint
}

type Application struct {
	log      *slog.Logger
	dir      string
	preserve uint
}

// imagePrefix is the prefix prepended to image names. This is used to track what
// and clean up old images downloaded by the app
const imagePrefix = "gnome-spotlight_"

func main() {
	var config Config
	flag.BoolVar(&config.debug, "debug", false, "Enable debug logging")
	flag.StringVar(
		&config.dir,
		"dir",
		path.Join(os.Getenv("HOME"), ".local/share/backgrounds"),
		"Directory for saving images",
	)
	flag.UintVar(
		&config.preserve,
		"preserve",
		3,
		("Number of previous images to preserve. If the number of saved images " +
			"would exceed this amount, the oldest image is deleted. Setting this " +
			"to 0 preserves all images."),
	)
	flag.Parse()

	level := slog.LevelInfo
	if config.debug {
		level = slog.LevelDebug
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	app := &Application{
		log:      log,
		dir:      config.dir,
		preserve: config.preserve,
	}

	if err := app.Run(); err != nil {
		log.Error("runtime error", "error", err)
		os.Exit(1)
	}
}

// Run is the main entrypoint of the application
func (a *Application) Run() error {
	path, err := a.newImage()
	if err != nil {
		return fmt.Errorf("new image: %w", err)
	}

	if err := a.writeToDconf(path); err != nil {
		return fmt.Errorf("write to dconf: %w", err)
	}

	if err := a.cleanImages(a.preserve); err != nil {
		return fmt.Errorf("clean images: %w", err)
	}

	return nil
}

// cleanImages deletes old images if current number is higher than preserve threshold
func (a *Application) cleanImages(preserve uint) error {
	// 0 means keep all
	if preserve == 0 {
		return nil
	}

	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}

	var files []os.FileInfo
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), imagePrefix) {
			a.log.Debug("found managed image", "value", entry.Name())

			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("get file info: %w", err)
			}

			files = append(files, info)
		}
	}

	if len(files) <= int(preserve) {
		return nil
	}

	a.log.Info("found more images than target amount, deleting oldest", "current", len(files), "target", preserve)

	slices.SortFunc(files, func(a, b os.FileInfo) int {
		return a.ModTime().Compare(b.ModTime())
	})

	for _, file := range files[:len(files)-int(preserve)] {
		a.log.Info("deleting image", "value", file.Name())

		if err := os.Remove(path.Join(a.dir, file.Name())); err != nil {
			return fmt.Errorf("delete image: %w", err)
		}
	}

	return nil
}

// writeToDconf sets dconf entries for background image to imagePath
func (a *Application) writeToDconf(imagePath string) error {
	keys := []string{
		"/org/gnome/desktop/background/picture-uri",
		"/org/gnome/desktop/background/picture-uri-dark",
		"/org/gnome/desktop/screensaver/picture-uri",
	}

	for _, key := range keys {
		// note quotes, this is necessary for dconf to recognize value as string
		value := fmt.Sprintf("'file://%s'", imagePath)

		a.log.Info("writing dconf entry", "key", key, "value", value)

		if _, err := exec.Command(
			"dconf",
			"write",
			key,
			value,
		).Output(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return fmt.Errorf("execute dconf write: %w: %s", err, exitErr.Stderr)
			}
			return fmt.Errorf("execute dconf write: %w", err)
		}
	}

	return nil
}

// newImages downloads a new image
func (a *Application) newImage() (string, error) {
	api := api.NewMicrosoft(a.log)
	url, err := api.Get()
	if err != nil {
		return "", fmt.Errorf("error getting image url: %w", err)
	}

	a.log.Info("fetched new image from api")

	a.log.Debug("extraced image url from response", "value", url)

	res, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch image: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("received non-ok response code when fetching image: %d", res.StatusCode)
	}

	a.log.Info("downloaded image")

	info, err := os.Stat(a.dir)
	if err != nil {
		return "", fmt.Errorf("stat image directory: %w", err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("dir exists but is not a directory")
	}

	path := path.Join(a.dir, imagePrefix+path.Base(url))

	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("image already exists")
	}

	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create image file: %w", err)
	}

	n, err := io.Copy(file, res.Body)
	if err != nil {
		return "", fmt.Errorf("write image file: %w", err)
	}

	a.log.Info("wrote image to file", "bytes", n, "path", path)

	return path, nil
}
