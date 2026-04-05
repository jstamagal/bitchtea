package ui

// BitchX-style ANSI art splash screen for bitchtea
// This is the soul of the application. Without it, we're just another terminal app.

const SplashArt = "" +
	"\033[1;36m" +
	"    ▄▄▄▄    ██▓▄▄▄█████▓ ▄████▄   ██░ ██ ▄▄▄█████▓▓█████  ▄▄▄      \n" +
	"   ▓█████▄ ▓██▒▓  ██▒ ▓▒▒██▀ ▀█  ▓██░ ██▒▓  ██▒ ▓▒▓█   ▀ ▒████▄    \n" +
	"   ▒██▒ ▄██▒██▒▒ ▓██░ ▒░▒▓█    ▄ ▒██▀▀██░▒ ▓██░ ▒░▒███   ▒██  ▀█▄  \n" +
	"   ▒██░█▀  ░██░░ ▓██▓ ░ ▒▓▓▄ ▄██▒░▓█ ░██ ░ ▓██▓ ░ ▒▓█  ▄ ░██▄▄▄▄██ \n" +
	"   ░▓█  ▀█▓░██░  ▒██▒ ░ ▒ ▓███▀ ░░▓█▒░██▓  ▒██▒ ░ ░▒████▒ ▓█   ▓██▒\n" +
	"   ░▒▓███▀▒░▓    ▒ ░░   ░ ░▒ ▒  ░ ▒ ░░▒░▒  ▒ ░░   ░░ ▒░ ░ ▒▒   ▓▒█░\n" +
	"   ▒░▒   ░  ▒ ░    ░      ░  ▒    ▒ ░▒░ ░    ░     ░ ░  ░  ▒   ▒▒ ░\n" +
	"    ░    ░  ▒ ░  ░      ░         ░  ░░ ░  ░         ░     ░   ▒   \n" +
	"    ░       ░           ░ ░       ░  ░  ░           ░  ░      ░  ░\n" +
	"                        ░                                         \n" +
	"\033[0m"

const SplashTagline = "" +
	"\033[1;35m  « bitchtea » \033[0;37m— putting the BITCH back in your terminal since 2026\033[0m\n" +
	"\033[0;36m  an agentic coding harness for people who don't need hand-holding\033[0m\n"

const ConnectMsg = "" +
	"\033[1;33m  *** \033[1;37mConnecting to %s...\033[0m\n" +
	"\033[1;33m  *** \033[1;37mUsing model: \033[1;32m%s\033[0m\n" +
	"\033[1;33m  *** \033[1;37mWorking directory: \033[1;34m%s\033[0m\n"

const MOTD = "" +
	"\033[0;90m  ─────────────────────────────────────────────────────────────\033[0m\n" +
	"\033[0;37m  Type a message to start coding. Use \033[1;33m/help\033[0;37m for commands.\033[0m\n" +
	"\033[0;37m  \033[1;33m/model\033[0;37m to switch models. \033[1;33m/quit\033[0;37m to exit. Don't be a wimp.\033[0m\n" +
	"\033[0;90m  ─────────────────────────────────────────────────────────────\033[0m\n"
