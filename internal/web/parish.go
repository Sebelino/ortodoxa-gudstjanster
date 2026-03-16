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
		Tradition: "Grekisk-ortodox",
		MapQuery:  "St+Georgios+Cathedral+Birger+Jarlsgatan+92+Stockholm",
	},
	{
		Slug:      "kristi-forklarings",
		Name:      "Kristi Förklarings Ortodoxa Församling",
		ShortName: "Kristi Förklarings",
		Address:   "Birger Jarlsgatan 98, Stockholm",
		City:      "Stockholm",
		Website:   "https://www.ryskaortodoxakyrkan.se",
		Languages: []string{"Kyrkoslaviska", "Svenska"},
		Tradition: "Rysk-ortodox",
		MapQuery:  "Birger+Jarlsgatan+98+Stockholm",
	},
	{
		Slug:      "heliga-anna",
		Name:      "Heliga Anna av Novgorod",
		ShortName: "Heliga Anna",
		Address:   "Kyrkvägen 27, Stocksund",
		City:      "Stocksund",
		Website:   "https://heligaanna.nu",
		Languages: []string{"Svenska"},
		Tradition: "Ortodox (Ekumeniska patriarkatet)",
		MapQuery:  "Kyrkvägen+27+Stocksund",
	},
	{
		Slug:      "finska-ortodoxa",
		Name:      "Finska Ortodoxa Församlingen",
		ShortName: "Helige Nikolai",
		Address:   "Köpmangatan 3, Gamla Stan, Stockholm",
		City:      "Stockholm",
		Website:   "https://www.ortodox-finsk.se",
		Languages: []string{"Svenska", "Finska"},
		Tradition: "Finsk-ortodox",
		MapQuery:  "Köpmangatan+3+Gamla+Stan+Stockholm",
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
	},
}

var parishBySlug = func() map[string]ParishInfo {
	m := make(map[string]ParishInfo, len(parishes))
	for _, p := range parishes {
		m[p.Slug] = p
	}
	return m
}()
