#!/usr/bin/env bash

reset="\033[0m"
bold="\033[1m"
dim="\033[2m"
red="\033[31m"
green="\033[32m"
yellow="\033[33m"
cyan="\033[36m"
fluo_yellow="\033[38;5;226m"
bright_red="\033[91m"
orange="\033[38;5;208m"
light_gray="\033[37m"
bright_white="\033[97m"

logo="${fluo_yellow}PRE${reset}${bright_red}≋${reset}${orange}≈${reset}${yellow}~${reset}${light_gray}∿${reset}"

type_cmd() {
  local text="$1"
  local i=0
  printf "${dim}\$${reset} "
  while [ $i -lt ${#text} ]; do
    printf "%s" "${text:$i:1}"
    sleep 0.03
    i=$((i + 1))
  done
  printf "\n"
}

scanning_clean() {
  sleep 0.5
  printf "${dim}scanning 1 package(s)...${reset}\n"
  sleep 0.3
  printf "${dim}● 1 packages clean${reset}\n"
}

summary_line() {
  printf "${dim}────────────────────${reset}\n"
  printf "${red}■${reset} 1 crit${dim} · ${reset}${yellow}▲${reset} 0 warn${dim} · ${reset}${cyan}⬆${reset} 0 ups${dim} · ${reset}${green}●${reset} ${bright_white}0 cached${reset}${dim} · ${reset}1 tots\n"
}

prompt_line() {
  printf "${cyan}?${reset} ${bold}Proceed with install?${reset} ${dim}[y/N]${reset} "
  sleep 2
  printf "\n"
}

printf "\n"

# Scene 1 — npm clean
type_cmd "npm install lodash"
scanning_clean
sleep 1.5
printf "\n"

# Scene 2 — npm vulnerable
type_cmd "npm install minimist@0.0.8"
sleep 0.5
printf "${dim}scanning 1 package(s)...${reset}\n"
sleep 0.3
printf "${logo}\n"
printf "${cyan}◆${reset} ${cyan}checking 1 package(s) (npm)${reset}\n"
sleep 0.1
printf "${dim}└── ${reset}${bold}minimist@0.0.8${reset}  ${red}■${reset} ${red}1 vulnerabilit(ies)${reset}\n"
sleep 0.1
printf "${dim}    └── ${reset}CVE-2021-44906       7.5  Prototype Pollution\n"
sleep 0.1
summary_line
printf "${yellow}┌─ ${red}Critical${reset}${yellow} ──────────────────────────────────────────────┐${reset}\n"
printf "${yellow}│ minimist@0.0.8                 CVE-2021-44906 7.5  HIGH  │${reset}\n"
printf "${yellow}└─────────────────────────────────────────────────────────┘${reset}\n"
sleep 0.1
prompt_line
sleep 1.5
printf "\n"

# Scene 3 — pip clean
type_cmd "pip install requests"
scanning_clean
sleep 1.5
printf "\n"

# Scene 4 — pip vulnerable
type_cmd "pip install urllib3==1.24.1"
sleep 0.5
printf "${dim}scanning 1 package(s)...${reset}\n"
sleep 0.3
printf "${logo}\n"
printf "${cyan}◆${reset} ${cyan}checking 1 package(s) (PyPI)${reset}\n"
sleep 0.1
printf "${dim}└── ${reset}${bold}urllib3@1.24.1${reset}  ${red}■${reset} ${red}2 vulnerabilit(ies)${reset}\n"
sleep 0.1
printf "${dim}    ├── ${reset}GHSA-2xpw-w6gg-jr37  7.5  Streaming decompression DoS\n"
sleep 0.1
printf "${dim}    └── ${reset}CVE-2019-11324       7.5  Mishandled SSL certificate\n"
sleep 0.1
summary_line
printf "${yellow}┌─ ${red}Critical${reset}${yellow} ────────────────────────────────────────────────────┐${reset}\n"
printf "${yellow}│ urllib3@1.24.1                  GHSA-2xpw-w6gg-jr37 7.5  HIGH │${reset}\n"
printf "${yellow}└─────────────────────────────────────────────────────────────┘${reset}\n"
sleep 0.1
prompt_line
sleep 1.5
printf "\n"

# Scene 5 — go clean
type_cmd "go get golang.org/x/text@latest"
scanning_clean
sleep 1.5
printf "\n"

# Scene 6 — go vulnerable
type_cmd "go get golang.org/x/net@v0.0.0-20200301022130-244492dfa37a"
sleep 0.5
printf "${dim}scanning 1 package(s)...${reset}\n"
sleep 0.3
printf "${logo}\n"
printf "${cyan}◆${reset} ${cyan}checking 1 package(s) (Go)${reset}\n"
sleep 0.1
printf "${dim}└── ${reset}${bold}golang.org/x/net@v0.0.0-20200301${reset}  ${red}■${reset} ${red}3 vulnerabilit(ies)${reset}\n"
sleep 0.1
printf "${dim}    ├── ${reset}CVE-2023-39325       7.5  HTTP/2 rapid reset DoS\n"
sleep 0.1
printf "${dim}    ├── ${reset}CVE-2023-3978        6.1  Improper rendering in html\n"
sleep 0.1
printf "${dim}    └── ${reset}CVE-2022-41723       7.5  HTTP/2 HPACK panic\n"
sleep 0.1
summary_line
printf "${yellow}┌─ ${red}Critical${reset}${yellow} ─────────────────────────────────────────────────────────────┐${reset}\n"
printf "${yellow}│ golang.org/x/net@v0.0.0-20200301   CVE-2023-39325 7.5  HIGH         │${reset}\n"
printf "${yellow}└──────────────────────────────────────────────────────────────────────┘${reset}\n"
sleep 0.1
prompt_line
printf "\n"

printf "${dim}────────────────────────────────────────${reset}\n"
printf "That's ${bold}pre${reset}. Catches known CVEs before they reach your project.\n\n"
printf "  ${cyan}brew install yowainwright/tap/pre${reset}\n\n"
