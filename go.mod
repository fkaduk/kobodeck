module gitlab.com/anarcat/wallabako

go 1.15

replace github.com/Strubbl/wallabago/v6 v6.0.0+incompatible => github.com/simonfrey/wallabago/v6 v6.0.7-0.20210117162249-afac782761b4

require (
	github.com/Strubbl/wallabago/v6 v6.0.6
	github.com/dustin/go-humanize v1.0.1
	github.com/mattn/go-sqlite3 v1.14.22
	github.com/nightlyone/lockfile v1.0.0
	golang.org/x/sys v0.24.0 // indirect
	golang.org/x/tools v0.24.0 // indirect
	golang.org/x/xerrors v0.0.0-20240716161551-93cc26a95ae9 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	lukechampine.com/uint128 v1.3.0 // indirect
	modernc.org/gc/v3 v3.0.0-20240801135723-a856999a2e4a // indirect
	modernc.org/libc v1.59.9 // indirect
	modernc.org/sqlite v1.32.0
	modernc.org/tcl v1.11.2 // indirect
)
