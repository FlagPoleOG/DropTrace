# DropTrace

DropTrace is a free Windows app that logs internet dropouts, shows where each one most
likely happened, and saves a PDF report you can send to your internet provider.

- See your PC, router, internet, and website lookups on one live screen.
- Log every outage with its start time, length, and likely cause.
- Save a PDF of the outages where your router was working but the internet wasn't.
- Keep your history on your PC. There's no account or tracking.

If DropTrace helps you, you can [support it on Ko-fi](https://ko-fi.com/flagpoleog).

DropTrace is made by [Second Shift Software](https://flagpoleog.github.io), along with [FolderTrace](https://apps.microsoft.com/detail/9NG4J9PKM2WC) and HitchTrace (coming soon).

Need more detail? [DropTrace Pro](https://apps.microsoft.com/detail/9PDG877VL6GP) is the paid version. It adds packet loss and jitter tracking, problem patterns, and Wi-Fi and route tools.

## Install

Get [DropTrace from the Microsoft Store](https://apps.microsoft.com/detail/9P0S0B1RCX70).

You can also download `DropTrace.exe` from the [Releases page](https://github.com/FlagPoleOG/DropTrace/releases) and run it. It doesn't need an installer
or administrator rights. Windows may show a SmartScreen warning because direct downloads
aren't signed. To continue, choose **More info**, then **Run anyway**.

DropTrace runs on 64-bit Windows 10 and 11.

## How the cause is decided

| PC has a route | Router answers | Public servers answer | Lookups work | Likely cause |
|---|---|---|---|---|
| Yes | Any | Yes | Yes | No problem |
| Yes | Any | Yes | No | Website lookups (DNS) |
| No | Not tested | Not tested | Not tested | This PC or its connection |
| Yes | Yes | No | Any | Internet provider or modem |
| Yes | No, but it has answered before | No | Any | Router or home network |
| Yes | No, and it has never answered | No | Any | Unclear |

These are likely causes, not proof. For example, DropTrace can't tell your modem from your
provider's network. Two failed checks start an outage, and two good checks end it. Time
while the PC is asleep or off isn't counted.

## How checks work

While the window is open, DropTrace sends one ping to the router and one to a public server
every second. In the tray, it does this every 3 seconds. If a ping fails, it runs a full check
right away, and it also runs one every 30 seconds. A full check pings 1.1.1.1, 8.8.8.8 and
9.9.9.9, looks up three website names, and tries a TLS handshake only if every ping failed.

See [PRIVACY.md](PRIVACY.md) for what this means for your data.

## Build

Install Go 1.24 or newer, then run these in Command Prompt:

    set GOOS=windows
    set GOARCH=amd64
    go build -trimpath -ldflags "-s -w -H=windowsgui" -o DropTrace.exe .

`rsrc_windows_amd64.syso` holds the app icon and Windows manifest, and Go links it
automatically. `packaging/build-msix.sh` builds the Microsoft Store package.

To test each outage type without unplugging anything, build with `-tags droptrace_sim` and
edit `sim.json` in the data folder. `go test ./...` covers the checks, outage logic, storage,
CSV export, and PDF report.

## License

[MIT](LICENSE)
