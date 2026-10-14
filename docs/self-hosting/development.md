# Working on the code

This page sets up a live-reloading copy of the website next to the Go server, so that you can change yoink and see the result without rebuilding everything.

This page assumes you have finished [Installing yoink](install.md).

## Instructions

### Section I - Running the server and the website separately

In this section, you will run the Go API and the Vite dev server side by side.

1. Open a terminal in the `yoink` folder
1. Run `make run`
    + This builds and starts the Go server, just like `./yoink`. You should see `listening on :3001`.
    + Rerun it after changing Go code.
1. Open a second terminal in the `yoink/frontend` folder
1. Run `npm ci` if you have not built the website before
1. Run `npm run dev`
    + You should see `VITE v6… ready in … ms`, then `➜  Local:   http://localhost:5173/`.
    + If `http://localhost:5173` shows a different app, another program already uses port 5173. Press `Ctrl` + `C` and run `npm run dev -- --port 5174` instead. Vite does not always notice a taken port.
1. Open `http://localhost:5173`
    + Changes to files in `frontend/src` show up in the browser as soon as you save.
    + Vite forwards `/api` requests to `localhost:3001`. If the Vite terminal shows `[vite] http proxy error: /api/…` with `ECONNREFUSED`, the Go server is not running, or you changed `PORT` in `.env`. Start it, or change the `target` in `frontend/vite.config.js` to match.

### Section II - Checking your changes

In this section, you will run the same checks a pull request needs.

1. Run `go vet ./...` from the `yoink` folder
    + No output means no problems.
1. Run `go test ./...`
    + Every package should say `ok` or `no test files`.
    + The format-selection tests run your installed yt-dlp against offline fixtures. They skip themselves when yt-dlp is missing.
1. Run `make build`
    + This rebuilds the website into `frontend/public`. Commit that folder along with your source changes, because existing deployments serve it as-is.

<details><summary>Folder layout (optional)</summary>

```
yoink/
├── cmd/yoink/        server entry point
├── cmd/bot/          Discord bot entry point
├── internal/         Go packages: routes, services, config, util
├── frontend/src/     Svelte source for the website
├── frontend/static/  images, icons and other files copied as-is
├── frontend/public/  built website (output of npm run build)
├── extractor/        TikTok/Instagram browser extractor (Node.js)
└── scripts/          transcribe.py and other helpers
```

</details>

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
