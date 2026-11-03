# DropTrace Pro privacy policy

Last updated: November 3, 2026

DropTrace Pro has no account, ads, analytics, or telemetry. Second Shift Software doesn't receive anything from the app, and the app never sends your history anywhere on its own.

## What stays on your PC

DropTrace Pro saves these in your Windows user folder:

- Outage and connection quality history, including times, likely causes, response times, packet loss, and adapter error counts.
- Your router's local IP address and hardware address, your DNS servers, and your network adapter's name and driver version.
- Wi-Fi details, including network name, access point address, signal, and channel. See "Wi-Fi and location."
- Routes to the internet and to any targets you add, including router addresses along the way and their names.
- Windows network event messages and the titles of installed Windows updates, so outages can be compared with changes on your PC.
- Your settings, notes, targets, Automatic Actions, Lag Test results, and error logs.
- The name and provider you type on the Report screen, so you don't need to retype them.

Your account number and monthly bill are used only to make the report or letter you're creating. They aren't saved in your history.

You can choose how long history is kept, or delete it, in Settings > Data. Delete history removes your outage, quality, route, Wi-Fi, change, and Lag Test history. Your settings, targets, Automatic Actions, and the name and provider on the Report screen stay until you change them. Backups and exported files you save are yours to manage. Uninstalling may not remove the data folder.

## Files you share

Nothing leaves your PC unless you send it. PDF reports, CSV files, credit request letters, and backups can contain the details above. Review a file before sharing it.

## What connects to the internet

To test your connection, DropTrace Pro contacts:

- Your router.
- Public servers at 1.1.1.1 (Cloudflare), 8.8.8.8 (Google), and 9.9.9.9 (Quad9), using pings and sometimes an encrypted connection test that sends no personal data.
- Your configured DNS service, to look up www.microsoft.com, www.cloudflare.com, and dns.google, and to find the names of routers on a route.
- Routers along the path to the internet, using route traces.
- Any game servers or sites you add as targets.

These services can see your public IP address and when the connection was made. Their own privacy policies apply.

## Lag Test

The Lag Test only runs when you agree and start it. It uses Measurement Lab (M-Lab) servers. M-Lab publishes each test's results and your public IP address in its public data set, and they can't be removed later. M-Lab's own privacy policy applies. Your history isn't included in the test.

## Automatic Actions

If you set up an action, DropTrace Pro runs the program or opens the web address you chose when its trigger happens. Whatever you connect it to receives that request.

## Wi-Fi and location

Windows treats Wi-Fi details as location information because they can reveal where you are, so it may ask for location permission before DropTrace Pro can read them. DropTrace Pro uses them only for Wi-Fi Doctor and outage details. It doesn't use Windows' location service or try to work out where you are. If you turn this off in Windows' location settings, Wi-Fi Doctor stops reading these details and the rest of the app keeps working.

## Links

**Support on Ko-fi** opens Ko-fi in your browser, and the **Read M-Lab's privacy policy** button opens M-Lab's site. Those sites' privacy policies apply. Tips are optional and don't unlock anything.

## Contact

Questions: [open an issue on GitHub](https://github.com/FlagPoleOG/DropTrace/issues).
