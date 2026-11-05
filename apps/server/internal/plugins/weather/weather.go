// Package weather is the built-in weather plugin's service, run inside
// OpenChat (plugins.LocalScheme): places, forecasts and past weather from
// Open-Meteo (open-meteo.com), which needs no key. Open-Meteo is free for
// non-commercial use; a commercial site needs its paid plan.
package weather

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qunqin24/openchat/apps/server/internal/plugins/localkit"
)

const (
	geocodeURL  = "https://geocoding-api.open-meteo.com/v1/search"
	forecastURL = "https://api.open-meteo.com/v1/forecast"
	archiveURL  = "https://archive-api.open-meteo.com/v1/archive"
	maxHistory  = 366
)

// Open-Meteo's free plan allows 600 requests a minute; one every 100 ms
// keeps the whole process well under.
var client = &localkit.Client{Gap: 100 * time.Millisecond}

// NewServer is the service, with its tools.
func NewServer(o localkit.Options) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "weather", Title: "Weather", Version: o.Version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: "find_place",
		Description: "Find a place by name (a city, town or district, in any language) and get its coordinates, " +
			"country, region and time zone. Several places can share a name: tell them apart by region and population.",
		Annotations: localkit.ReadOnly,
	}, findPlace)
	mcp.AddTool(server, &mcp.Tool{
		Name: "forecast",
		Description: "The weather now and the forecast for up to 16 days, for a place by name or by coordinates: " +
			"temperature, feels-like, precipitation and its probability, wind, humidity, UV, sunrise and sunset. " +
			"Set hourly for the next 48 hours hour by hour. Times are the place's local time.",
		Annotations: localkit.ReadOnly,
	}, forecast)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "history",
		Description: "Past weather, day by day, for a place by name or by coordinates, from 1940 to a few days ago; up to 366 days at a time.",
		Annotations: localkit.ReadOnly,
	}, history)
	return server
}

// ---------------------------------------------------------------- places

type Place struct {
	Name       string  `json:"name"`
	Region     string  `json:"region,omitempty"`
	Country    string  `json:"country,omitempty"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Timezone   string  `json:"timezone,omitempty"`
	Population int     `json:"population,omitempty"`
	Elevation  float64 `json:"elevation_m,omitempty"`
}

type FindPlaceInput struct {
	Name     string `json:"name" jsonschema:"the place's name"`
	Language string `json:"language,omitempty" jsonschema:"the language of the names returned, such as zh or en; en when absent"`
	Count    int    `json:"count,omitempty" jsonschema:"1 to 10; 5 when absent"`
}

type PlaceList struct {
	Places []Place `json:"places"`
}

var languagePattern = regexp.MustCompile(`^[a-z]{2}$`)

func places(ctx context.Context, name, language string, count int) ([]Place, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("name is empty")
	}
	if language == "" {
		language = "en"
	}
	if !languagePattern.MatchString(language) {
		return nil, errors.New("language is a two-letter code such as zh or en")
	}
	if count <= 0 {
		count = 5
	}
	var r struct {
		Results []struct {
			Name       string  `json:"name"`
			Latitude   float64 `json:"latitude"`
			Longitude  float64 `json:"longitude"`
			Elevation  float64 `json:"elevation"`
			Timezone   string  `json:"timezone"`
			Population int     `json:"population"`
			Country    string  `json:"country"`
			Admin1     string  `json:"admin1"`
			Admin2     string  `json:"admin2"`
		} `json:"results"`
	}
	params := url.Values{"name": {name}, "count": {fmt.Sprint(min(count, 10))}, "language": {language}, "format": {"json"}}
	if err := client.JSON(ctx, geocodeURL+"?"+params.Encode(), nil, &r); err != nil {
		return nil, err
	}
	out := []Place{}
	for _, p := range r.Results {
		region := p.Admin1
		if p.Admin2 != "" && p.Admin2 != p.Admin1 && p.Admin2 != p.Name {
			region = p.Admin2 + ", " + p.Admin1
		}
		out = append(out, Place{Name: p.Name, Region: region, Country: p.Country, Latitude: p.Latitude, Longitude: p.Longitude,
			Timezone: p.Timezone, Population: p.Population, Elevation: p.Elevation})
	}
	return out, nil
}

func findPlace(ctx context.Context, _ *mcp.CallToolRequest, in FindPlaceInput) (*mcp.CallToolResult, PlaceList, error) {
	found, err := places(ctx, in.Name, in.Language, in.Count)
	if err == nil && len(found) == 0 {
		err = fmt.Errorf("no place is called %q; try another spelling, or the name in English", in.Name)
	}
	return nil, PlaceList{Places: found}, err
}

// Where is a place by name or by coordinates, as forecast and history take it.
type Where struct {
	Place     string   `json:"place,omitempty" jsonschema:"a place's name; the best-known place of that name is used"`
	Latitude  *float64 `json:"latitude,omitempty" jsonschema:"with longitude, instead of place"`
	Longitude *float64 `json:"longitude,omitempty"`
}

// locate is where to ask about, and the place found when asked by name.
func (w Where) locate(ctx context.Context) (lat, lon float64, place *Place, err error) {
	if w.Latitude != nil && w.Longitude != nil {
		if *w.Latitude < -90 || *w.Latitude > 90 || *w.Longitude < -180 || *w.Longitude > 180 {
			return 0, 0, nil, errors.New("latitude is -90 to 90 and longitude -180 to 180")
		}
		return *w.Latitude, *w.Longitude, nil, nil
	}
	if strings.TrimSpace(w.Place) == "" {
		return 0, 0, nil, errors.New("give a place, or latitude and longitude")
	}
	// The geocoder matches a name in the language asked for: the one its
	// script is written in.
	found, err := places(ctx, w.Place, scriptLanguage(w.Place), 1)
	if err != nil {
		return 0, 0, nil, err
	}
	if len(found) == 0 {
		return 0, 0, nil, fmt.Errorf("no place is called %q; find it with find_place, or give coordinates", w.Place)
	}
	return found[0].Latitude, found[0].Longitude, &found[0], nil
}

// ---------------------------------------------------------------- forecast

type ForecastInput struct {
	Where
	Days   int  `json:"days,omitempty" jsonschema:"how many days, 1 to 16; 7 when absent"`
	Hourly bool `json:"hourly,omitempty" jsonschema:"also the next 48 hours, hour by hour"`
}

type Current struct {
	Time          string  `json:"time"`
	Weather       string  `json:"weather"`
	Temperature   float64 `json:"temperature_c"`
	FeelsLike     float64 `json:"feels_like_c"`
	Humidity      float64 `json:"humidity_pct"`
	Precipitation float64 `json:"precipitation_mm"`
	Wind          float64 `json:"wind_kmh"`
	Gusts         float64 `json:"gusts_kmh"`
	WindFrom      string  `json:"wind_from"`
	IsDay         bool    `json:"is_day"`
}

type Day struct {
	Date          string   `json:"date"`
	Weather       string   `json:"weather"`
	Max           float64  `json:"max_c"`
	Min           float64  `json:"min_c"`
	Precipitation float64  `json:"precipitation_mm"`
	RainChance    *int     `json:"precipitation_probability_pct,omitempty"`
	Wind          float64  `json:"max_wind_kmh"`
	UV            *float64 `json:"uv_index_max,omitempty"`
	Sunrise       string   `json:"sunrise,omitempty"`
	Sunset        string   `json:"sunset,omitempty"`
}

type Hour struct {
	Time          string  `json:"time"`
	Weather       string  `json:"weather"`
	Temperature   float64 `json:"temperature_c"`
	RainChance    int     `json:"precipitation_probability_pct"`
	Precipitation float64 `json:"precipitation_mm"`
	Wind          float64 `json:"wind_kmh"`
}

type ForecastOutput struct {
	Place     *Place   `json:"place,omitempty" jsonschema:"the place found by name"`
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
	Timezone  string   `json:"timezone"`
	Current   *Current `json:"current,omitempty"`
	Daily     []Day    `json:"daily"`
	Hourly    []Hour   `json:"hourly,omitempty"`
}

type forecastAnswer struct {
	Timezone string `json:"timezone"`
	Current  struct {
		Time          string  `json:"time"`
		Temperature   float64 `json:"temperature_2m"`
		FeelsLike     float64 `json:"apparent_temperature"`
		Humidity      float64 `json:"relative_humidity_2m"`
		Precipitation float64 `json:"precipitation"`
		Code          int     `json:"weather_code"`
		Wind          float64 `json:"wind_speed_10m"`
		Gusts         float64 `json:"wind_gusts_10m"`
		Direction     float64 `json:"wind_direction_10m"`
		IsDay         int     `json:"is_day"`
	} `json:"current"`
	Daily struct {
		Time          []string   `json:"time"`
		Code          []int      `json:"weather_code"`
		Max           []float64  `json:"temperature_2m_max"`
		Min           []float64  `json:"temperature_2m_min"`
		Precipitation []float64  `json:"precipitation_sum"`
		Chance        []*int     `json:"precipitation_probability_max"`
		Wind          []float64  `json:"wind_speed_10m_max"`
		UV            []*float64 `json:"uv_index_max"`
		Sunrise       []string   `json:"sunrise"`
		Sunset        []string   `json:"sunset"`
	} `json:"daily"`
	Hourly struct {
		Time          []string  `json:"time"`
		Temperature   []float64 `json:"temperature_2m"`
		Chance        []int     `json:"precipitation_probability"`
		Precipitation []float64 `json:"precipitation"`
		Code          []int     `json:"weather_code"`
		Wind          []float64 `json:"wind_speed_10m"`
	} `json:"hourly"`
}

func coordinates(lat, lon float64) url.Values {
	return url.Values{"latitude": {fmt.Sprintf("%.4f", lat)}, "longitude": {fmt.Sprintf("%.4f", lon)}, "timezone": {"auto"}}
}

func forecast(ctx context.Context, _ *mcp.CallToolRequest, in ForecastInput) (*mcp.CallToolResult, ForecastOutput, error) {
	lat, lon, place, err := in.Where.locate(ctx)
	if err != nil {
		return nil, ForecastOutput{}, err
	}
	days := in.Days
	if days <= 0 {
		days = 7
	}
	days = min(days, 16)
	params := coordinates(lat, lon)
	params.Set("forecast_days", fmt.Sprint(days))
	params.Set("current", "temperature_2m,apparent_temperature,relative_humidity_2m,precipitation,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,is_day")
	params.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum,precipitation_probability_max,wind_speed_10m_max,uv_index_max,sunrise,sunset")
	if in.Hourly {
		params.Set("hourly", "temperature_2m,precipitation_probability,precipitation,weather_code,wind_speed_10m")
		params.Set("forecast_hours", "48")
	}
	var a forecastAnswer
	if err := client.JSON(ctx, forecastURL+"?"+params.Encode(), nil, &a); err != nil {
		return nil, ForecastOutput{}, err
	}
	c := a.Current
	out := ForecastOutput{
		Place: place, Latitude: lat, Longitude: lon, Timezone: a.Timezone,
		Current: &Current{
			Time: c.Time, Weather: describe(c.Code), Temperature: c.Temperature, FeelsLike: c.FeelsLike, Humidity: c.Humidity,
			Precipitation: c.Precipitation, Wind: c.Wind, Gusts: c.Gusts, WindFrom: compass(c.Direction), IsDay: c.IsDay == 1,
		},
		Daily: dailyOf(a),
	}
	h := a.Hourly
	for i := range h.Time {
		out.Hourly = append(out.Hourly, Hour{
			Time: h.Time[i], Weather: describe(at(h.Code, i)), Temperature: at(h.Temperature, i),
			RainChance: at(h.Chance, i), Precipitation: at(h.Precipitation, i), Wind: at(h.Wind, i),
		})
	}
	return nil, out, nil
}

func dailyOf(a forecastAnswer) []Day {
	d := a.Daily
	out := []Day{}
	for i := range d.Time {
		day := Day{
			Date: d.Time[i], Weather: describe(at(d.Code, i)), Max: at(d.Max, i), Min: at(d.Min, i),
			Precipitation: at(d.Precipitation, i), Wind: at(d.Wind, i), Sunrise: clock(at(d.Sunrise, i)), Sunset: clock(at(d.Sunset, i)),
		}
		if i < len(d.Chance) {
			day.RainChance = d.Chance[i]
		}
		if i < len(d.UV) {
			day.UV = d.UV[i]
		}
		out = append(out, day)
	}
	return out
}

// at is v[i], or nothing when the answer is shorter.
func at[T any](v []T, i int) T {
	var zero T
	if i < len(v) {
		return v[i]
	}
	return zero
}

// clock is the time of day of an ISO time.
func clock(iso string) string {
	if i := strings.Index(iso, "T"); i >= 0 {
		return iso[i+1:]
	}
	return iso
}

// ---------------------------------------------------------------- history

type HistoryInput struct {
	Where
	Start string `json:"start_date" jsonschema:"the first day, YYYY-MM-DD"`
	End   string `json:"end_date" jsonschema:"the last day, YYYY-MM-DD; at most 366 days after the first"`
}

type HistoryOutput struct {
	Place     *Place  `json:"place,omitempty"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
	Daily     []Day   `json:"daily"`
}

func history(ctx context.Context, _ *mcp.CallToolRequest, in HistoryInput) (*mcp.CallToolResult, HistoryOutput, error) {
	start, err1 := time.Parse(time.DateOnly, in.Start)
	end, err2 := time.Parse(time.DateOnly, in.End)
	switch {
	case err1 != nil || err2 != nil:
		return nil, HistoryOutput{}, errors.New("start_date and end_date are days, YYYY-MM-DD")
	case end.Before(start):
		return nil, HistoryOutput{}, errors.New("end_date is before start_date")
	case end.Sub(start) > maxHistory*24*time.Hour:
		return nil, HistoryOutput{}, fmt.Errorf("at most %d days at a time", maxHistory)
	}
	lat, lon, place, err := in.Where.locate(ctx)
	if err != nil {
		return nil, HistoryOutput{}, err
	}
	params := coordinates(lat, lon)
	params.Set("start_date", in.Start)
	params.Set("end_date", in.End)
	params.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum,wind_speed_10m_max,sunrise,sunset")
	var a forecastAnswer
	if err := client.JSON(ctx, archiveURL+"?"+params.Encode(), nil, &a); err != nil {
		return nil, HistoryOutput{}, err
	}
	return nil, HistoryOutput{Place: place, Latitude: lat, Longitude: lon, Timezone: a.Timezone, Daily: dailyOf(a)}, nil
}

// scriptLanguage is the language a place's name is most likely in, from its
// letters: Japanese kana, Korean hangul, Chinese characters, else English.
func scriptLanguage(name string) string {
	han := false
	for _, r := range name {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			return "ja"
		case unicode.Is(unicode.Hangul, r):
			return "ko"
		case unicode.Is(unicode.Han, r):
			han = true
		case unicode.Is(unicode.Cyrillic, r):
			return "ru"
		}
	}
	if han {
		return "zh"
	}
	return "en"
}

// ---------------------------------------------------------------- words

// describe is a WMO weather code in words.
func describe(code int) string {
	switch code {
	case 0:
		return "clear sky"
	case 1:
		return "mainly clear"
	case 2:
		return "partly cloudy"
	case 3:
		return "overcast"
	case 45, 48:
		return "fog"
	case 51, 53, 55:
		return "drizzle"
	case 56, 57:
		return "freezing drizzle"
	case 61:
		return "light rain"
	case 63:
		return "rain"
	case 65:
		return "heavy rain"
	case 66, 67:
		return "freezing rain"
	case 71:
		return "light snow"
	case 73:
		return "snow"
	case 75:
		return "heavy snow"
	case 77:
		return "snow grains"
	case 80, 81:
		return "rain showers"
	case 82:
		return "violent rain showers"
	case 85, 86:
		return "snow showers"
	case 95:
		return "thunderstorm"
	case 96, 99:
		return "thunderstorm with hail"
	}
	return fmt.Sprintf("weather code %d", code)
}

// compass is where wind from degrees comes from.
func compass(degrees float64) string {
	points := []string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}
	i := int((degrees+22.5)/45) % 8
	if i < 0 {
		i += 8
	}
	return points[i]
}
