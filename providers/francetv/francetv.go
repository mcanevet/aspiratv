package francetv

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/simulot/aspiratv/net/http"
	"github.com/simulot/aspiratv/providers"
)

// init registers FranceTV provider
func init() {
	p, err := New()
	if err != nil {
		panic(err)
	}
	providers.Register(p)
}

// Provider constants
const (
	ProviderName = "francetv"
	WSListURL    = "http://pluzz.webservices.francetelevisions.fr/pluzz/liste/type/replay/nb/%d/debut/0"           // Available show
	WSInfoOeuvre = "http://webservices.francetelevisions.fr/tools/getInfosOeuvre/v2/?catalogue=Pluzz&idDiffusion=" // Show's video link and details
)

type getter interface {
	Get(uri string) (io.ReadCloser, error)
}

// FranceTV structure handles france-tv catalog of shows
type FranceTV struct {
	getter getter
}

// WithGetter inject a getter in FranceTV object instead of normal one
func WithGetter(g getter) func(ftv *FranceTV) {
	return func(ftv *FranceTV) {
		ftv.getter = g
	}
}

// New setup a Show provider for France Télévisions
func New(conf ...func(ftv *FranceTV)) (*FranceTV, error) {
	ftv := &FranceTV{
		getter: http.DefaultClient,
	}
	for _, fn := range conf {
		fn(ftv)
	}
	return ftv, nil
}

// Name return the name of the provider
func (ftv FranceTV) Name() string { return "francetv" }

// Shows return shows that match with matching list.
func (ftv *FranceTV) Shows(mm []*providers.MatchRequest) chan *providers.Show {
	shows := make(chan *providers.Show)

	go func() {
		defer close(shows)
		url := fmt.Sprintf(WSListURL, 3000) // Limit to the last 3000th shows

		// Get JSON catalog of available shows on France Télévisions
		r, err := ftv.getter.Get(url)
		if err != nil {
			log.Printf("[%s] Can't call catalog API: %q", err)
			return
		}
		// r = httptest.DumpReaderToFile(r, "francetv-catalog-")
		defer r.Close()

		d := json.NewDecoder(r)
		list := &pluzzList{}
		err = d.Decode(list)
		if err != nil {
			log.Printf("[%s] Can't decode catalog: %q", err)
		}

		for _, e := range list.Reponse.Emissions {
			// Map JSON object to provider.Show common structure
			show := &providers.Show{
				ID:           e.IDDiffusion,
				Show:         strings.TrimSpace(e.Titre),
				Title:        strings.TrimSpace(e.Soustitre),
				Season:       e.Saison,
				Episode:      e.Episode,
				Pitch:        strings.TrimSpace(e.Accroche),
				AirDate:      time.Time(e.TsDiffusionUtc),
				Channel:      e.ChaineID,
				Detailed:     false,
				DRM:          false, //TBD
				Duration:     time.Duration(e.DureeReelle),
				Category:     strings.TrimSpace(e.Rubrique),
				Provider:     ProviderName,
				ShowURL:      e.OasSitepage,
				StreamURL:    "", // Must call GetShowStreamURL to get the show's URL
				ThumbnailURL: e.ImageLarge,
			}
			if providers.IsShowMatch(mm, show) {
				shows <- show
			}
		}
	}()
	return shows
}

// GetShowStreamURL return the show's URL, a m3u8 playlist
func (ftv *FranceTV) GetShowStreamURL(s *providers.Show) (string, error) {
	if s.StreamURL == "" {
		err := ftv.GetShowInfo(s)
		if err != nil {
			return "", fmt.Errorf("Can't get detailed information for the show: %v", err)
		}
	}
	return s.StreamURL, nil
}

// GetShowInfo query the URL from InfoOeuvre web service
func (ftv *FranceTV) GetShowInfo(s *providers.Show) error {
	if s.Detailed {
		return nil
	}
	i := infoOeuvre{}

	url := WSInfoOeuvre + s.ID
	r, err := ftv.getter.Get(url)
	if err != nil {
		return fmt.Errorf("Can't get show's detailed information: %v", err)
	}
	// r = httptest.DumpReaderToFile(r, "francetv-info-"+s.ID+"-")
	err = json.NewDecoder(r).Decode(&i)
	if err != nil {
		return fmt.Errorf("Can't decode show's detailed information: %v", err)
	}

	s.ThumbnailURL = i.ImageSecure
	for _, v := range i.Videos {
		if v.Format == "hls_v5_os" {
			s.StreamURL = v.URL
			break
		}
	}
	if s.StreamURL == "" {
		return fmt.Errorf("Can't find hls_v5_os stream for the show")
	}
	s.Detailed = true
	return nil
}

// GetShowFileName return a file name with a path that is compatible with PLEX server:
//   ShowName/Season NN/ShowName - sNNeMM - Episode title
//   Show and Episode names are sanitized to avoid problem when saving on the file system
func (FranceTV) GetShowFileName(s *providers.Show) string {

	var showPath, seasonPath, episodePath string
	showPath = providers.PathNameCleaner(s.Show)

	if s.Season == "" {
		seasonPath = "Season " + s.AirDate.Format("2006")
	} else {
		seasonPath = "Season " + providers.Format2Digits(s.Season)
	}

	if s.Episode == "" {
		episodePath = providers.FileNameCleaner(s.Show) + " - " + s.AirDate.Format("2006-01-02")
	} else {
		episodePath = providers.FileNameCleaner(s.Show) + " - s" + providers.Format2Digits(s.Season) + "e" + providers.Format2Digits(s.Episode)
	}

	if s.Episode == "" && (s.Title == "" || s.Title == s.Show) {
		episodePath += " - " + s.ID + ".mp4"
	} else {
		if s.Title != "" && s.Title != s.Show {
			episodePath += " - " + providers.FileNameCleaner(s.Title) + ".mp4"
		} else {
			episodePath += ".mp4"
		}
	}

	return filepath.Join(showPath, seasonPath, episodePath)

}

// GetShowFileNameMatcher return a file pattern of this show
// used for detecting already got episode even when episode or season is different
func (p *FranceTV) GetShowFileNameMatcher(s *providers.Show) string {
	return p.GetShowFileName(s)
}
