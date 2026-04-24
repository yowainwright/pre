#!/usr/bin/env bash

reset="\033[0m"
bold="\033[1m"
dim="\033[2m"
red="\033[31m"
cyan="\033[36m"
yellow="\033[33m"
fluo_yellow="\033[38;5;226m"
bright_red="\033[91m"
orange="\033[38;5;208m"
light_gray="\033[37m"

logo="${fluo_yellow}PRE${reset}${bright_red}≋${reset}${orange}≈${reset}${yellow}~${reset}${light_gray}∿${reset}"

type_cmd() {
  local text="$1" i=0
  printf "${dim}\$${reset} "
  while [ "$i" -lt "${#text}" ]; do
    printf "%s" "${text:$i:1}"
    sleep 0.03
    i=$((i + 1))
  done
  printf "\n"
}

eval "$(sed -n '/^# pre security proxy/,$ p' ~/.bashrc 2>/dev/null)"

printf "\n  ${logo}  ${dim}security scanning for package managers${reset}\n\n"
sleep 1

printf "${dim}# pre setup writes hooks into your shell — works with any package manager${reset}\n"
type_cmd "grep -A6 'function npm' ~/.bashrc"
grep -A6 'function npm' ~/.bashrc 2>/dev/null
printf "\n"
sleep 2

printf "${dim}# clean install — pre scans before npm runs${reset}\n"
type_cmd "npm install lodash@4.17.21"
npm install lodash@4.17.21
printf "\n"
sleep 2

printf "${dim}# known CVE — pre catches it before anything installs${reset}\n"
type_cmd "npm install minimist@0.0.8"
printf 'n\n' | npm install minimist@0.0.8
printf "\n"
sleep 2

printf "${dim}# pip is hooked too — cross-ecosystem, same protection${reset}\n"
type_cmd "pip install urllib3==1.24.1"
printf 'n\n' | pip install urllib3==1.24.1
printf "\n"
sleep 2

type_cmd "pre status"
pre status

printf "\n${dim}──────────────────────────────────────────────────────────${reset}\n"
printf "Install  ${cyan}brew install yowainwright/tap/pre${reset}\n"
printf "Docs     ${dim}github.com/yowainwright/pre${reset}\n\n"

exec bash -i
