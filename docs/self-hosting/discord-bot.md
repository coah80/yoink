# Discord bot

This page runs yoink's Discord bot, so that people in your server can use `/yoink`, `/convert` and `/compress` and get the file back in Discord.

This page assumes you have finished [Installing yoink](install.md) and yoink is running.

<details><summary>Technical details (optional)</summary>

- The bot is a second program, `yoink-bot`. It does no downloading itself: it sends each job to the yoink server at `YOINK_API_URL`, signed with `BOT_SECRET`.
- Files too big for Discord are sent as a link built from `YOINK_PUBLIC_URL`, so that address has to be reachable by the people clicking it.
- The bot keeps its settings in `bot-data.db`, in the folder you start it from.

</details>

## What you need

- A Discord account with permission to add bots to your server (`Manage Server`)

## Instructions

### Section I - Creating the Discord application

In this section, you will create the bot account on Discord and copy its credentials.

> [!NOTE]
> Discord changes its portal labels from time to time. These steps follow Discord's [getting started guide](https://discord.com/developers/docs/quick-start/getting-started). If a label differs, look there.

1. Open the [Discord Developer Portal](https://discord.com/developers/applications)
1. Click `New Application`, enter a name, accept the terms and click `Create`
    + You land on the `General Information` page.
1. Copy the `Application ID`
    + This is your `DISCORD_APP_ID`, a long number.
1. Navigate to `Bot`
1. Click `Reset Token`, confirm, then copy the token
    + This is your `DISCORD_TOKEN`. Discord shows it only once.
1. Under `Privileged Gateway Intents`, turn on `Server Members Intent` and `Message Content Intent`
1. Click `Save Changes`
    + Without these two intents the bot stops on start with `close 4014: Disallowed intent(s)`.
1. Navigate to `Installation`
1. Under `Default Install Settings` -> `Guild Install`, add the scopes `applications.commands` and `bot`
1. In the `Permissions` menu that appears, select `Send Messages`, `Attach Files` and `Embed Links`, then save
1. Copy the `Install Link`, open it in your browser, and add the bot to your server
    + The bot shows up in the server's member list as offline until you start it in Section II.

> [!CAUTION]
> Anyone with the bot token **controls your bot**. Keep it only in `.env`, and never commit it or paste it in chat.

### Section II - Configuring and starting the bot

In this section, you will connect the bot to your yoink server and start it.

1. Run `openssl rand -hex 32` and copy the output
    + You should see 64 letters and digits. This is your `BOT_SECRET`, the shared password between the bot and yoink.
1. Open `.env` in the `yoink` folder and set:

   ```ini
   BOT_SECRET=<the-value-from-step-1>
   DISCORD_TOKEN=<your-bot-token>
   DISCORD_APP_ID=<your-application-id>
   YOINK_API_URL=http://localhost:3001
   YOINK_PUBLIC_URL=http://localhost:3001
   ```

    + Change `YOINK_PUBLIC_URL` to your public address (for example `https://yoink.example.com`) if other people will use the bot. With `localhost`, download links only work on your own machine.
1. Stop yoink with `Ctrl` + `C`, then run `./yoink` again
    + The `BOT_SECRET not set` warning is gone.
1. Open a new terminal in the `yoink` folder
1. Run `make bot`
    + You should see one `go build` line, and a `yoink-bot` file appears in the folder.
1. Run `./yoink-bot`
    + The bot reads `.env` from the folder you run it in, so run it from the `yoink` folder.
    + You should see `[DB] Initialized database at bot-data.db`, then `Bot is running. Press Ctrl+C to stop.`
    + If you see `DISCORD_TOKEN is required`, `DISCORD_APP_ID is required` or `BOT_SECRET is required`, that line is missing from `.env`.
    + If you see `Failed to start bot: websocket: close 4004: Authentication failed.`, the token is wrong. Repeat [Section I, step 5](#section-i---creating-the-discord-application).
    + If you see `Failed to start bot: websocket: close 4014: Disallowed intent(s)`, return to [Section I, step 6](#section-i---creating-the-discord-application).
1. Type `/yoink` in your Discord server and paste a link
    + If the commands do not show up, reload Discord with `Ctrl` + `R` (`Cmd` + `R` on macOS).
    + If the bot answers `Download Failed` with `Unauthorized`, `BOT_SECRET` differs between the running yoink and the bot. Restart both after editing `.env`.

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
