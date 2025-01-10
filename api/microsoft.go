package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

const apiUrl = "https://fd.api.iris.microsoft.com/v4/api/selection?&placement=88000820&bcnt=1&country=%s&locale=%s&fmt=json"

type microsoft struct {
	log *slog.Logger
}

func NewMicrosoft(log *slog.Logger) API {
	return &microsoft{log: log}
}

// body is the content of the parsed response body
type body struct {
	Batchrsp struct {
		Items []struct {
			Item string
		}
	}
}

// metadata is the metadata of the image
type metadata struct {
	Ad struct {
		LandscapeImage struct {
			Asset string
		}
	}
}

func (api *microsoft) Get() (string, error) {
	lang := os.Getenv("LANG")

	api.log.Debug("read LANG variable", "value", lang)

	locale := ""
	if l := strings.Split(lang, "."); len(l) != 0 {
		locale = strings.ReplaceAll(l[0], "_", "-")
	} else {
		return "", fmt.Errorf("failed to parse locale from LANG: %s", l)
	}

	country := ""
	if c := strings.Split(locale, "-"); len(c) != 0 {
		country = c[len(c)-1]
	} else {
		return "", fmt.Errorf("failed to parse country code from locale: %s", locale)
	}

	api.log.Debug("determined localization", "locale", locale, "country", country)

	url := fmt.Sprintf(apiUrl, country, locale)

	api.log.Debug("calling api", "url", url)

	res, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("invalid response when querying microsoft api: %w", err)
	}
	defer res.Body.Close()

	api.log.Debug("received api response")

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("received non-ok response code when querying microsoft api: %d", res.StatusCode)
	}

	var body body
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode microsoft api response body: %w", err)
	}

	if len(body.Batchrsp.Items) == 0 {
		return "", fmt.Errorf("microsoft api response body contains no images")
	}

	item := body.Batchrsp.Items[0].Item

	api.log.Debug("decoded image metadata", "value", item)

	var metadata metadata
	if err := json.NewDecoder(strings.NewReader(item)).Decode(&metadata); err != nil {
		return "", fmt.Errorf("decode microsoft api image metadata: %w", err)
	}

	return metadata.Ad.LandscapeImage.Asset, nil
}
