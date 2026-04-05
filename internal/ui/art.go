package ui

import (
	"math/rand"
)

// BitchX-style ANSI art splash screen for bitchtea
// This is the soul of the application. Without it, we're just another terminal app.

const splashArt1 = "" +
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

const splashArt2 = "" +
	"\033[1;31m" +
	"   ██████╗ ██╗████████╗ ██████╗██╗  ██╗████████╗███████╗ █████╗ \n" +
	"   ██╔══██╗██║╚══██╔══╝██╔════╝██║  ██║╚══██╔══╝██╔════╝██╔══██╗\n" +
	"   ██████╔╝██║   ██║   ██║     ███████║   ██║   █████╗  ███████║\n" +
	"   ██╔══██╗██║   ██║   ██║     ██╔══██║   ██║   ██╔══╝  ██╔══██║\n" +
	"   ██████╔╝██║   ██║   ╚██████╗██║  ██║   ██║   ███████╗██║  ██║\n" +
	"   ╚═════╝ ╚═╝   ╚═╝    ╚═════╝╚═╝  ╚═╝   ╚═╝   ╚══════╝╚═╝  ╚═╝\n" +
	"\033[0m"

const splashArt3 = "" +
	"\033[1;35m" +
	"   _     _ _       _     _              \n" +
	"  | |   (_) |     | |   | |             \n" +
	"  | |__  _| |_ ___| |__ | |_ ___  __ _  \n" +
	"  | '_ \\| | __/ __| '_ \\| __/ _ \\/ _` | \n" +
	"  | |_) | | || (__| | | | ||  __/ (_| | \n" +
	"  |_.__/|_|\\__\\___|_| |_|\\__\\___|\\__,_| \n" +
	"\033[0m" +
	"\033[0;90m" +
	"        your code's worst nightmare       \n" +
	"\033[0m"

const splashArt4 = "" +
	"\033[1;33m" +
	"  ┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓\n" +
	"  ┃\033[1;37m  ▒▒▒ ▒ ▒▒▒ ▒▒▒ ▒ ▒ ▒▒▒ ▒▒▒ ▒▒▒ ▒▒▒                     \033[1;33m┃\n" +
	"  ┃\033[1;37m  ▒▒▒ ▒  ▒  ▒   ▒▒▒  ▒  ▒   ▒▒▒  ▒  bitchtea v0.1.0    \033[1;33m┃\n" +
	"  ┃\033[1;37m  ▒▒▒ ▒  ▒  ▒▒▒ ▒ ▒  ▒  ▒▒▒ ▒ ▒  ▒  (c) 2026 jstamagal \033[1;33m┃\n" +
	"  ┃                                                           ┃\n" +
	"  ┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛\n" +
	"\033[0m"

var splashArts = []string{splashArt1, splashArt2, splashArt3, splashArt4}

// SplashArt returns a random ANSI art splash
func SplashArt() string {
	return splashArts[rand.Intn(len(splashArts))]
}

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
	"\033[0;37m  Use \033[1;33m@filename\033[0;37m to include file contents. \033[1;33m/auto-next\033[0;37m for autopilot.\033[0m\n" +
	"\033[0;90m  ─────────────────────────────────────────────────────────────\033[0m\n"
