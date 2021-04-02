package apps

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"golift.io/cnfg"
	"golift.io/starr"
	"golift.io/starr/radarr"
)

// radarrHandlers is called once on startup to register the web API paths.
func (a *Apps) radarrHandlers() {
	a.HandleAPIpath(Radarr, "/add", radarrAddMovie, "POST")
	a.HandleAPIpath(Radarr, "/check/{tmdbid:[0-9]+}", radarrCheckMovie, "GET")
	a.HandleAPIpath(Radarr, "/get/{movieid:[0-9]+}", radarrGetMovie, "GET")
	a.HandleAPIpath(Radarr, "/get", radarrGetAllMovies, "GET")
	a.HandleAPIpath(Radarr, "/qualityProfiles", radarrProfiles, "GET")
	a.HandleAPIpath(Radarr, "/rootFolder", radarrRootFolders, "GET")
	a.HandleAPIpath(Radarr, "/search/{query}", radarrSearchMovie, "GET")
	a.HandleAPIpath(Radarr, "/tag", radarrGetTags, "GET")
	a.HandleAPIpath(Radarr, "/tag/{tid:[0-9]+}/{label}", radarrUpdateTag, "PUT")
	a.HandleAPIpath(Radarr, "/tag/{label}", radarrSetTag, "PUT")
	a.HandleAPIpath(Radarr, "/update", radarrUpdateMovie, "PUT")
	a.HandleAPIpath(Radarr, "/exclusions", radarrGetExclusions, "GET")
	a.HandleAPIpath(Radarr, "/exclusions", radarrAddExclusions, "POST")
	a.HandleAPIpath(Radarr, "/exclusions/{eid:(?:[0-9],?)+}", radarrDelExclusions, "DELETE")
}

// RadarrConfig represents the input data for a Radarr server.
type RadarrConfig struct {
	Name     string
	Interval cnfg.Duration
	*starr.Config
	radarr *radarr.Radarr
}

func (r *RadarrConfig) setup(timeout time.Duration) {
	r.radarr = radarr.New(r.Config)
	if r.Timeout.Duration == 0 {
		r.Timeout.Duration = timeout
	}
}

func radarrAddMovie(r *http.Request) (int, interface{}) {
	var payload radarr.AddMovieInput
	// Extract payload and check for TMDB ID.
	err := json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("decoding payload: %w", err)
	} else if payload.TmdbID == 0 {
		return http.StatusUnprocessableEntity, fmt.Errorf("0: %w", ErrNoTMDB)
	}

	app := getRadarr(r)
	// Check for existing movie.
	m, err := app.GetMovie(payload.TmdbID)
	if err != nil {
		return http.StatusServiceUnavailable, fmt.Errorf("checking movie: %w", err)
	} else if len(m) > 0 {
		return http.StatusConflict, fmt.Errorf("%d: %w", payload.TmdbID, ErrExists)
	}

	if payload.Title == "" {
		// Title must exist, even if it's wrong.
		payload.Title = strconv.FormatInt(payload.TmdbID, 10)
	}

	if payload.MinimumAvailability == "" {
		payload.MinimumAvailability = "released"
	}

	// Add movie using fixed payload.
	movie, err := app.AddMovie(&payload)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("adding movie: %w", err)
	}

	return http.StatusCreated, movie
}

func radarrCheckMovie(r *http.Request) (int, interface{}) {
	tmdbID, _ := strconv.ParseInt(mux.Vars(r)["tmdbid"], 10, 64)
	// Check for existing movie.
	m, err := getRadarr(r).GetMovie(tmdbID)
	if err != nil {
		return http.StatusServiceUnavailable, fmt.Errorf("checking movie: %w", err)
	} else if len(m) > 0 {
		return http.StatusConflict, fmt.Errorf("%d: %w", tmdbID, ErrExists)
	}

	return http.StatusOK, http.StatusText(http.StatusNotFound)
}

func radarrGetMovie(r *http.Request) (int, interface{}) {
	movieID, _ := strconv.ParseInt(mux.Vars(r)["movieid"], 10, 64)

	movie, err := getRadarr(r).GetMovieByID(movieID)
	if err != nil {
		return http.StatusServiceUnavailable, fmt.Errorf("checking movie: %w", err)
	}

	return http.StatusOK, movie
}

func radarrGetAllMovies(r *http.Request) (int, interface{}) {
	movies, err := getRadarr(r).GetMovie(0)
	if err != nil {
		return http.StatusServiceUnavailable, fmt.Errorf("checking movie: %w", err)
	}

	return http.StatusOK, movies
}

func radarrProfiles(r *http.Request) (int, interface{}) {
	// Get the profiles from radarr.
	profiles, err := getRadarr(r).GetQualityProfiles()
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("getting profiles: %w", err)
	}

	// Format profile ID=>Name into a nice map.
	p := make(map[int64]string)
	for i := range profiles {
		p[profiles[i].ID] = profiles[i].Name
	}

	return http.StatusOK, p
}

func radarrRootFolders(r *http.Request) (int, interface{}) {
	// Get folder list from Radarr.
	folders, err := getRadarr(r).GetRootFolders()
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("getting folders: %w", err)
	}

	// Format folder list into a nice path=>freesSpace map.
	p := make(map[string]int64)
	for i := range folders {
		p[folders[i].Path] = folders[i].FreeSpace
	}

	return http.StatusOK, p
}

func radarrSearchMovie(r *http.Request) (int, interface{}) {
	// Get all movies
	movies, err := getRadarr(r).GetMovie(0)
	if err != nil {
		return http.StatusServiceUnavailable, fmt.Errorf("getting movies: %w", err)
	}

	query := strings.TrimSpace(strings.ToLower(mux.Vars(r)["query"])) // in
	returnMovies := make([]map[string]interface{}, 0)                 // out

	for _, movie := range movies {
		if movieSearch(query, []string{movie.Title, movie.OriginalTitle}, movie.AlternateTitles) {
			returnMovies = append(returnMovies, map[string]interface{}{
				"id":                  movie.ID,
				"title":               movie.Title,
				"cinemas":             movie.InCinemas,
				"status":              movie.Status,
				"exists":              movie.HasFile,
				"added":               movie.Added,
				"year":                movie.Year,
				"path":                movie.Path,
				"tmdbId":              movie.TmdbID,
				"qualityProfileId":    movie.QualityProfileID,
				"monitored":           movie.Monitored,
				"minimumAvailability": movie.MinimumAvailability,
			})
		}
	}

	return http.StatusOK, returnMovies
}

func movieSearch(query string, titles []string, alts []*radarr.AlternativeTitle) bool {
	for _, t := range titles {
		if t != "" && strings.Contains(strings.ToLower(t), query) {
			return true
		}
	}

	for _, t := range alts {
		if strings.Contains(strings.ToLower(t.Title), query) {
			return true
		}
	}

	return false
}

func radarrGetTags(r *http.Request) (int, interface{}) {
	tags, err := getRadarr(r).GetTags()
	if err != nil {
		return http.StatusServiceUnavailable, fmt.Errorf("getting tags: %w", err)
	}

	return http.StatusOK, tags
}

func radarrUpdateTag(r *http.Request) (int, interface{}) {
	id, _ := strconv.Atoi(mux.Vars(r)["tid"])

	tagID, err := getRadarr(r).UpdateTag(id, mux.Vars(r)["label"])
	if err != nil {
		return http.StatusServiceUnavailable, fmt.Errorf("updating tag: %w", err)
	}

	return http.StatusOK, tagID
}

func radarrSetTag(r *http.Request) (int, interface{}) {
	tagID, err := getRadarr(r).AddTag(mux.Vars(r)["label"])
	if err != nil {
		return http.StatusServiceUnavailable, fmt.Errorf("setting tag: %w", err)
	}

	return http.StatusOK, tagID
}

func radarrUpdateMovie(r *http.Request) (int, interface{}) {
	var movie radarr.Movie
	// Extract payload and check for TMDB ID.
	err := json.NewDecoder(r.Body).Decode(&movie)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("decoding payload: %w", err)
	}

	// Check for existing movie.
	err = getRadarr(r).UpdateMovie(movie.ID, &movie)
	if err != nil {
		return http.StatusServiceUnavailable, fmt.Errorf("updating movie: %w", err)
	}

	return http.StatusOK, "radarr seems to have worked"
}

func radarrAddExclusions(r *http.Request) (int, interface{}) {
	var exclusions []*radarr.Exclusion

	err := json.NewDecoder(r.Body).Decode(&exclusions)
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("decoding payload: %w", err)
	}

	// Get the profiles from radarr.
	err = getRadarr(r).AddExclusions(exclusions)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("adding exclusions: %w", err)
	}

	return http.StatusOK, "added " + strconv.Itoa(len(exclusions)) + " exclusions"
}

func radarrGetExclusions(r *http.Request) (int, interface{}) {
	exclusions, err := getRadarr(r).GetExclusions()
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("getting exclusions: %w", err)
	}

	return http.StatusOK, exclusions
}

func radarrDelExclusions(r *http.Request) (int, interface{}) {
	ids := mux.Vars(r)["eid"]
	exclusions := []int64{}

	for _, s := range strings.Split(ids, ",") {
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			exclusions = append(exclusions, i)
		}
	}

	// Get the profiles from radarr.
	err := getRadarr(r).DeleteExclusions(exclusions)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("deleting exclusions: %w", err)
	}

	return http.StatusOK, "deleted: " + strings.Join(strings.Split(ids, ","), ", ")
}
