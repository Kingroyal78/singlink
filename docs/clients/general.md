---
icon: material/pencil-ruler
---

# General

Describes and explains the functions implemented uniformly by singlink graphical clients.

### Profile

Profile describes a singlink configuration file and its state.

#### Local

* Local Profile represents a local singlink configuration with minimal state
* The graphical client must provide an editor to modify configuration content

#### iCloud (on iOS and macOS)

* iCloud Profile represents a remote singlink configuration with iCloud as the update source
* The configuration file is stored in the singlink folder under iCloud
* The graphical client must provide an editor to modify configuration content

#### Remote

* Remote Profile represents a remote singlink configuration with a URL as the update source.
* The graphical client should provide a configuration content viewer
* The graphical client must implement automatic profile update (default interval is 60 minutes) and HTTP Basic
  authorization.

At the same time, the graphical client must provide support for importing remote profiles
through a specific URL Scheme. The URL is defined as follows:

```
singlink://import-remote-profile?url=urlEncodedURL#urlEncodedName
```

### Dashboard

While the singlink service is running, the graphical client should provide a Dashboard interface to manage the service.

#### Status

Dashboard should display status information such as memory, connection, and traffic.

#### Mode

Dashboard should provide a Mode selector for switching when the configuration uses at least two `clash_mode` values.

#### Groups

When the configuration includes group outbounds (specifically, Selector or URLTest),
the dashboard should provide a Group selector for status display or switching.

### Chore

#### Core

Graphical clients should provide a Core region:

* Display the current singlink version
* Provides a button to clean the working directory
* Provides a memory limiter switch