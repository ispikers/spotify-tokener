# spotify-tokener

A simple GoLang service which returns spotify anonymous & account tokens without having to deal with their annoying TOTP shit.

## Installation

For god's sake, just use docker. Otherwise, have fun compiling yourself:

### Manual

Requirements:
- [go 1.26+](https://go.dev/doc/install)
- [chrome](https://www.google.com/chrome/) (only used as a fallback)

```bash
go install github.com/ispikers/spotify-tokener@master
```

Now run it via systemd or something.

### Docker

Create your `compose.yml` and run `docker compose up -d`.

```yaml
services:
    spotify-tokener:
        image: ghcr.io/ispikers/spotify-tokener:master
        container_name: spotify-tokener
        restart: unless-stopped
        environment:
        #      - SPOTIFY_TOKENER_ADDR=0.0.0.0:8080
        #      - SPOTIFY_TOKENER_CHROME_PATH=chrome
        #      - SPOTIFY_TOKENER_LOG_LEVEL=INFO
        ports:
            - 8080:8080
```


## Usage

The endpoint is under `/api/token` and cookies are relayed 1:1 (like the sp_dc cookie for account login).

Tokens are fetched directly using TOTP secrets from [spotify-secrets](https://code.thetadev.de/ThetaDev/spotify-secrets), falling back to headless Chrome. Outbound access to `code.thetadev.de` and `open.spotify.com` is required.

## License

This project is licensed under the [Apache License 2.0](LICENSE).

This is a modified fork of [topi314/spotify-tokener](https://github.com/topi314/spotify-tokener). The files have been changed from the original; see the git history for what changed.

## Contributing

Contributions are welcome, but for bigger changes, please open an issue first to discuss what you would like to change.