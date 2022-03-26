module gitlab.com/anarcat/wallabako

go 1.15

replace github.com/Strubbl/wallabago/v6 v6.0.0+incompatible => github.com/simonfrey/wallabago/v6 v6.0.7-0.20210117162249-afac782761b4

require (
	github.com/Strubbl/wallabago/v6 v6.0.6
	github.com/dustin/go-humanize v1.0.0
	github.com/mattn/go-sqlite3 v1.14.12
	github.com/nightlyone/lockfile v1.0.0
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
)
