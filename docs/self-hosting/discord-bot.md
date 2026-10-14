# Discord bot

This page runs yoink's Discord bot, so that people in your server can use `/yoink`, `/convert` and `/compress` and get the file back in Discord.

This page assumes you have finished [Installing yoink](install.md) and yoink is running.

<details><summary>Technical details (optional)</summary>

The bot is a second binary, `yoink-bot`. It does no downloading itself: it sends each job to the yoink API at `YOINK_API_URL`, signed with `BOT_SECRET`. Files too big for Discord are sent as a link built from `YOINK_PUBLIC_URL`, so that address has to be reachable by the people clicking it.

</details>

## What you need

- A Discord account with permission to add bots to your server (`Manage Server`)

## Instructions

### Section I - Creating the Discord application

In this section, you will create the bot account on Discord and copy its credentials.

1. Open the [Discord Developer Portal](https://discord.com/developers/applications)
1. Click `New Application`, name it, and click `Create`
1. Copy the `Application ID` from the `General Information` page
    + This is your `DISCORD_APP_ID`.
1. Navigate to `Bot`
1. Click `Reset Token`, then copy the token
    + This is your `DISCORD_TOKEN`. Discord only shows it once.
1. Under `Privileged Gateway Intents`, turn on `Server Members Intent` and `Message Content Intent`
1. Click `Save Changes`
    + If you skip this step, the bot exits on start with `Disallowed intent(s)`.
1. Navigate to `OAuth2` -> `URL Generator`
1. Tick `bot` and `applications.commands`, then under bot permissions tick `Send Messages`, `Attach Files` and `Embed Links`
1. Open the generated URL and add the bot to your server

> [!CAUTION]
> Anyone with the bot token **controls your bot**. Keep it only in `.env`, and never commit it or paste it in chat.

### Section II - Configuring and starting the bot

In this section, you will connect the bot to your yoink server and start it.

1. Run `openssl rand -hex 32` and copy the output
    + You should see a 64-character string of letters and digits. This is your `BOT_SECRET`, the shared password between the bot and yoink.
    + If you see `openssl: command not found`, use any long random string instead.
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
    + The `BOT_SECRET not set` warning should be gone.
1. Open a new terminal in the `yoink` folder
1. Run `make bot`
    + You should see one `go build` line, and a `yoink-bot` file appears in the folder.
1. Run `./yoink-bot`
    + The bot reads `.env` from the folder you run it in, so run it from the `yoink` folder.
    + You should see `Bot is running. Press Ctrl+C to stop.`
    + If you see `DISCORD_TOKEN is required` or `BOT_SECRET is required`, that line is missing from `.env`.
    + If you see `Disallowed intent(s)`, return to [Section I, step 6](#section-i---creating-the-discord-application).
1. Type `/yoink` in your Discord server and paste a link
    + New slash commands can take a minute to appear. Restart Discord with `Ctrl` + `R` if they do not.
    + If the bot replies with an authorization error, `BOT_SECRET` differs between the running yoink and the bot. Restart both after editing `.env`.

---

> [!TIP]
> Return to [Running yoink yourself](README.md)
