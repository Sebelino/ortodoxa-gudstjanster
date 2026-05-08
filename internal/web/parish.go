package web

// ParishInfo holds static information about an Orthodox parish.
type ParishInfo struct {
	Slug               string
	Name               string
	ShortName          string
	Address            string
	City               string
	County             string // short county name used in filter params, e.g. "Stockholm"
	Website            string
	PrimaryLanguage    string   // main liturgical language; used for language filtering of unlabelled events
	SecondaryLanguages []string // additional languages used at this parish
	Tradition          string
	MapQuery           string
	Lat                float64
	Lng                float64
}

var parishes = []ParishInfo{
	{
		Slug:               "st-georgios",
		Name:               "St. Georgios Cathedral",
		ShortName:          "St. Georgios",
		Address:            "Birger Jarlsgatan 92, Stockholm",
		City:               "Stockholm",
		County:             "Stockholm",
		Website:            "https://gomos.se",
		PrimaryLanguage:    "Grekiska",
		SecondaryLanguages: []string{"Svenska", "Engelska"},
		Tradition:          "Grekisk-ortodox (Ekumeniska patriarkatet)",
		MapQuery:           "St+Georgios+Cathedral+Birger+Jarlsgatan+92+Stockholm",
		Lat:                59.34604475278758,
		Lng:                18.06271002636969,
	},
	{
		Slug:               "kristi-forklarings",
		Name:               "Kristi Förklarings Ortodoxa Församling",
		ShortName:          "Kristi Förklaring",
		Address:            "Birger Jarlsgatan 98, Stockholm",
		City:               "Stockholm",
		County:             "Stockholm",
		Website:            "https://www.ryskaortodoxakyrkan.se",
		PrimaryLanguage:    "Kyrkoslaviska",
		SecondaryLanguages: []string{"Svenska"},
		Tradition:          "Rysk-ortodox (Bulgariska patriarkatet)",
		MapQuery:           "Birger+Jarlsgatan+98+Stockholm",
		Lat:                59.34675752739769,
		Lng:                18.06185982086056,
	},
	{
		Slug:            "heliga-anna",
		Name:            "Heliga Anna av Novgorod",
		ShortName:       "Heliga Anna",
		Address:         "Kyrkvägen 27, Stocksund",
		City:            "Stocksund",
		County:          "Stockholm",
		Website:         "https://heligaanna.nu",
		PrimaryLanguage: "Svenska",
		Tradition:       "Svensk-ortodox (Georgiska patriarkatet)",
		MapQuery:        "Kyrkvägen+27+Stocksund",
		Lat:             59.39017384201317,
		Lng:             18.057616987012704,
	},
	{
		Slug:               "finska-ortodoxa",
		Name:               "Finska Ortodoxa Församlingen",
		ShortName:          "Helige Nikolai",
		Address:            "Bellmansgatan 13, Stockholm",
		City:               "Stockholm",
		County:             "Stockholm",
		Website:            "https://www.ortodox-finsk.se",
		PrimaryLanguage:    "Finska",
		SecondaryLanguages: []string{"Svenska"},
		Tradition:          "Finsk-ortodox (Ekumeniska patriarkatet)",
		MapQuery:           "Bellmansgatan+13+Stockholm",
		Lat:                59.31843100230095,
		Lng:                18.066269644544022,
	},
	{
		Slug:               "st-ignatios",
		Name:               "St. Ignatios",
		ShortName:          "St. Ignatios",
		Address:            "Nygatan 2, Södertälje",
		City:               "Södertälje",
		County:             "Stockholm",
		Website:            "https://heligaanna.nu",
		PrimaryLanguage:    "Svenska",
		SecondaryLanguages: []string{"Grekiska", "Serbiska"},
		Tradition:          "Svensk-ortodox (Georgiska patriarkatet)",
		MapQuery:           "Sankt+Ignatios+Folkhögskola+Nygatan+2+Södertälje",
		Lat:                59.1955,
		Lng:                17.6253,
	},
	{
		Slug:            "sankt-sava",
		Name:            "Sankt Sava",
		ShortName:       "Sankt Sava",
		Address:         "Bägerstavägen 68, Enskede",
		City:            "Enskede",
		County:          "Stockholm",
		Website:         "https://www.crkvastokholm.se",
		PrimaryLanguage: "Kyrkoslaviska",
		Tradition:       "Serbisk-ortodox",
		MapQuery:        "Bägerstavägen+68+Enskede",
		Lat:             59.289587434290844,
		Lng:             18.061649082707426,
	},
	{
		Slug:               "sankt-goran",
		Name:               "Sankt Göran",
		ShortName:          "Sankt Göran",
		Address:            "Vanadisvägen 35, Stockholm",
		City:               "Stockholm",
		County:             "Stockholm",
		Website:            "https://borss.se",
		PrimaryLanguage:    "Rumänska",
		SecondaryLanguages: []string{"Svenska", "Engelska"},
		Tradition:          "Rumänsk-ortodox",
		MapQuery:           "Matteus+Lillkyrkan+Vanadisvägen+35+Stockholm",
		Lat:                59.3454446,
		Lng:                18.0424408,
	},
	{
		Slug:            "kristi-uppstandelse",
		Name:            "Kristi Uppståndelses Ortodoxa församling",
		ShortName:       "Kristi Uppståndelse",
		City:            "Göteborg",
		County:          "Västra Götaland",
		Website:         "https://kristiuppstandelse.se",
		PrimaryLanguage: "Svenska",
		Tradition:       "Antiokiansk-ortodox (Antiokias Ortodoxa Patriarkat)",
	},
}

var countyNames = map[string]string{
	"Stockholm":      "Stockholms län",
	"Västra Götaland": "Västra Götalands län",
}

func countyDisplayName(county string) string {
	if name, ok := countyNames[county]; ok {
		return name
	}
	return county
}

var parishBySlug = func() map[string]ParishInfo {
	m := make(map[string]ParishInfo, len(parishes))
	for _, p := range parishes {
		m[p.Slug] = p
	}
	return m
}()
