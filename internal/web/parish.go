package web

// ParishInfo holds static information about an Orthodox parish.
type ParishInfo struct {
	Slug       string
	Name       string
	ShortName  string
	Address    string
	City       string
	Website    string
	Languages  []string
	Tradition  string
	MapQuery   string
	Lat        float64
	Lng        float64
}

var parishes = []ParishInfo{
	{
		Slug:      "st-georgios",
		Name:      "St. Georgios Cathedral",
		ShortName: "St. Georgios",
		Address:   "Birger Jarlsgatan 92, Stockholm",
		City:      "Stockholm",
		Website:   "https://gomos.se",
		Languages: []string{"Grekiska", "Svenska", "Engelska"},
		Tradition: "Grekisk-ortodox (Ekumeniska patriarkatet)",
		MapQuery:  "St+Georgios+Cathedral+Birger+Jarlsgatan+92+Stockholm",
		Lat:       59.3402,
		Lng:       18.0743,
	},
	{
		Slug:      "kristi-forklarings",
		Name:      "Kristi Förklarings Ortodoxa Församling",
		ShortName: "Kristi Förklarings",
		Address:   "Birger Jarlsgatan 98, Stockholm",
		City:      "Stockholm",
		Website:   "https://www.ryskaortodoxakyrkan.se",
		Languages: []string{"Kyrkoslaviska", "Svenska"},
		Tradition: "Rysk-ortodox (Bulgariska patriarkatet)",
		MapQuery:  "Birger+Jarlsgatan+98+Stockholm",
		Lat:       59.3410,
		Lng:       18.0745,
	},
	{
		Slug:      "heliga-anna",
		Name:      "Heliga Anna av Novgorod",
		ShortName: "Heliga Anna",
		Address:   "Kyrkvägen 27, Stocksund",
		City:      "Stocksund",
		Website:   "https://heligaanna.nu",
		Languages: []string{"Svenska"},
		Tradition: "Svensk-ortodox (Georgiska patriarkatet)",
		MapQuery:  "Kyrkvägen+27+Stocksund",
		Lat:       59.3856,
		Lng:       18.0548,
	},
	{
		Slug:      "finska-ortodoxa",
		Name:      "Finska Ortodoxa Församlingen",
		ShortName: "Helige Nikolai",
		Address:   "Bellmansgatan 13, Stockholm",
		City:      "Stockholm",
		Website:   "https://www.ortodox-finsk.se",
		Languages: []string{"Svenska", "Finska"},
		Tradition: "Finsk-ortodox (Ekumeniska patriarkatet)",
		MapQuery:  "Bellmansgatan+13+Stockholm",
		Lat:       59.3190,
		Lng:       18.0668,
	},
	{
		Slug:      "sankt-sava",
		Name:      "Sankt Sava",
		ShortName: "Sankt Sava",
		Address:   "Bägerstavägen 68, Enskede",
		City:      "Enskede",
		Website:   "https://www.crkvastokholm.se",
		Languages: []string{"Kyrkoslaviska"},
		Tradition: "Serbisk-ortodox",
		MapQuery:  "Bägerstavägen+68+Enskede",
		Lat:       59.2770,
		Lng:       18.0710,
	},
}

var parishBySlug = func() map[string]ParishInfo {
	m := make(map[string]ParishInfo, len(parishes))
	for _, p := range parishes {
		m[p.Slug] = p
	}
	return m
}()
