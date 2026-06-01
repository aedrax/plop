# plop

`plop` (Pull, Link, Organize, Place) is a simple, zero-dependency command line tool that downloads, extracts, and installs user-space applications (tarballs, zip files, AppImages, and standalone binaries) on Linux.

It sandboxes everything under `~/.local/opt/` and creates symlinks in `~/.local/bin/` so you can run them immediately in your shell.

## Why?

Managing pre-compiled binaries on Linux is annoying. You usually have to download a tarball from GitHub, extract it, manually copy files into `/usr/local/bin` (which pollutes your system and requires sudo), or write desktop shortcuts by hand.

`plop` automates this process cleanly in your home directory:
*   **Zero Dependencies**: Written in pure Go, using only the standard library.
*   **Zero Trace Uninstall**: A single command cleanly sweeps away the app folder, binary symlinks, and desktop entries.
*   **Desktop Specs Support**: Automatically rewrites or generates compliant `.desktop` entries and maps custom icons.
*   **Upgrades Built-in**: Checks for and pulls updates from GitHub releases, generic HTML download pages, or custom source-checking scripts.

## Installation

Ensure `~/go/bin` or `~/.local/bin` is in your `$PATH`.

### Direct Go Install (Recommended)
You can install and compile the latest version of `plop` directly from GitHub using Go:

```bash
go install github.com/aedrax/plop@latest
```

### Manual Compilation
Alternatively, clone the repository and build it locally:

```bash
# Clone the repository
git clone https://github.com/aedrax/plop.git
cd plop

# Compile and place the binary in your local bin directory
go build -o ~/.local/bin/plop .
```

## Screenshots Or It Didn't Happen

The following are some action shots of plop

### Install from archive:

<img width="1182" height="404" alt="Image" src="https://github.com/user-attachments/assets/8f28dcbd-7622-42f3-a027-8c61082f787d" />

### Check for updates:

<img width="1182" height="233" alt="Image" src="https://github.com/user-attachments/assets/474e04fc-1783-49ef-9dde-032c671a419f" />

### Perform Upgrade:

<img width="1182" height="507" alt="Image" src="https://github.com/user-attachments/assets/a001ccaa-edcb-4ee4-9cfd-4afe8975a8df" />

### List installed:

<img width="1172" height="178" alt="Image" src="https://github.com/user-attachments/assets/959c506b-0831-44f3-8345-f893cefaec4f" />

## Quick Start

### Installing an application
You can install from a local file, a direct URL, or standard input:

```bash
# Install a local AppImage or archive
plop install ~/Downloads/cutter-v2.4.1.AppImage

# Install directly from a remote download link
plop install https://github.com/ErikKalkoken/janice/releases/download/v0.10.0/janice-0.10.0-linux-amd64.tar.xz

# Install directly from a GitHub repository URL
plop install https://github.com/jesseduffield/lazygit

# Stream directly from a pipe
cat app.tar.gz | plop install -
```

### Managing updates
`plop` can check your installed apps against their remote sources and upgrade them:

```bash
# Check all installed apps for new versions
plop update

# Upgrade all apps that have updates available
plop upgrade

# Upgrade a specific app
plop upgrade lazygit
```

### Uninstallation
Completely purge an installed app and its symlinks:

```bash
plop uninstall lazygit
```

### Modifying metadata
If you want to rename an app, adjust its registered version, or change its update link after installation:

```bash
# Interactively edit app metadata (including deep filesystem renames)
plop set lazygit

# Or pass explicit flags
plop set lazygit --name git-lazy --version 0.41.0
```

## Custom Source Checking Plugins

For obscure applications or complex releases not hosted on standard platforms, `plop` allows you to register custom update checking plugins. 

By setting an application's update source URL to `plugin:///path/to/script.sh` (or `plugin://~/bin/script.sh`), `plop` will run your local script or executable whenever checking for updates (`plop update`) or upgrading (`plop upgrade`).

### Expected Script Output
Your plugin script must print the latest version and direct download link to `stdout`. It can do this in one of two formats:

#### 1. JSON (Recommended)
```json
{
  "version": "2.4.0",
  "url": "https://example.com/builds/app-linux-x86_64.tar.gz"
}
```

#### 2. Two-Line Plain Text
```text
2.4.0
https://example.com/builds/app-linux-x86_64.tar.gz
```

### Usage Example
To associate a plugin script during initial installation:

```bash
plop install ~/Downloads/app-v1.0.tar.gz --source "plugin://~/.local/bin/check-app.sh"
```

If the application is already installed, you can register or change its plugin update script at any time:

```bash
plop set myapp --source "plugin://~/.local/bin/check-app.sh"
```

## Custom Install Scripts

If an application is distributed as a source archive that requires compilation or custom post-extraction commands rather than shipping precompiled binaries, `plop` supports custom installation scripts.

When a custom script is registered, `plop` will extract the download package to a temporary directory and execute your script with three positional arguments:
1. `temp_extract_dir`: The directory containing the extracted package contents.
2. `target_app_dir`: The target sandboxed installation folder (`~/.local/opt/<app>`).
3. `bin_dir`: The user space binary directory (`~/.local/bin/`).

Your script is responsible for building/moving files into the sandboxed `target_app_dir`. Once completed, `plop` automatically takes over to handle the desktop shortcut, icon, and binary symlinking phases.

### Standard Script Lifecycle
When you upgrade an application via `plop upgrade`, the custom script path is preserved and executed automatically on the newly downloaded and extracted package, keeping your source builds fully automated.

### Usage Example
Specify a script during installation:

```bash
plop install ~/Downloads/src-app.zip --install-script "/path/to/build-script.sh"
```

If the application is already installed, you can add, change, or clear the build script path at any time:

```bash
# Set or edit the build script path
plop set myapp --install-script "/path/to/new-build-script.sh"

# Clear the script
plop set myapp --install-script none
```

## Configuration

Settings are stored in `~/.config/plop/config.json`. The registry of installed applications is kept in `~/.config/plop/registry.json`.

```json
{
  "opt_dir": "~/.local/opt",
  "bin_dir": "~/.local/bin",
  "apps_dir": "~/.local/share/applications",
  "icons_dir": "~/.local/share/icons",
  "github_token": "",
  "auto_confirm": false,
  "default_gui": null
}
```

*   `github_token`: Set a personal access token if you hit rate limits checking GitHub releases.
*   `auto_confirm`: Set to `true` to skip confirmations during install/upgrade.
*   `default_gui`: Set `true` to always generate desktop GUI shortcuts, or `false` to skip. Set to `null` to prompt.
