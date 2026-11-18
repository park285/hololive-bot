package constants

var HolodexAPIParams = struct {
	OrgHololive         string
	OrgVSpo             string
	OrgStellive         string
	OrgIndie            string
	OrgAll              string
	StatusLive          string
	StatusUpcoming      string
	TypeStream          string
	TypeVtuber          string
	MaxUpcomingHours    int
	StreamListLimit     int
	DefaultChannelLimit int
	MaxPaginationOffset int
	SyncTargetOrgs      []string
	AllowedFilterOrgs   []string
}{
	OrgHololive:         "Hololive",
	OrgVSpo:             "VSpo",
	OrgStellive:         "Stellive",
	OrgIndie:            "Independents",
	OrgAll:              "all",
	StatusLive:          "live",
	StatusUpcoming:      "upcoming",
	TypeStream:          "stream",
	TypeVtuber:          "vtuber",
	MaxUpcomingHours:    168,
	StreamListLimit:     50,
	DefaultChannelLimit: 50,
	MaxPaginationOffset: 500,
	SyncTargetOrgs:      []string{"Hololive", "VSpo", "Stellive"},
	AllowedFilterOrgs:   []string{"Hololive", "VSpo", "Independents", "Stellive"},
}

var IndieChannelIDs = []string{
	"UCrV1Hf5r8P148idjoSfrGEQ",
	"UCxsZ6NCzjU_t4YSxQLBcM5A",
	"UCt30jJgChL8qeT9VPadidSw",
}

var IndieChannelOrgOverrides = map[string]string{
	"UCt30jJgChL8qeT9VPadidSw": "Hololive",
}
