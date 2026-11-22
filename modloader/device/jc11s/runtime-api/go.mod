module prime-unlimited.runtime-api

go 1.24.0

require prime-unlimited.usb-copy/worker v0.0.0

require (
	github.com/dhowden/tag v0.0.0-20240417053706-3d75831295e8 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/tcolgate/mp3 v0.0.0-20170426193717-e79c5a46d300 // indirect
	golang.org/x/sys v0.22.0 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
	modernc.org/sqlite v1.34.5 // indirect
)

replace prime-unlimited.usb-copy/worker => "../../../../mods/USB Copy/src/worker"
