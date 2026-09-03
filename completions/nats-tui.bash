# bash completion for nats-tui: source this file, or install it as
# share/bash-completion/completions/nats-tui
_nats_tui() {
  local cur prev
  cur=${COMP_WORDS[COMP_CWORD]}
  prev=${COMP_WORDS[COMP_CWORD-1]}
  case $prev in
    -context)
      COMPREPLY=($(compgen -W "$(nats context ls --names 2>/dev/null)" -- "$cur")); return ;;
    -creds|-nats)
      COMPREPLY=($(compgen -f -- "$cur")); return ;;
    -s|-timeout)
      return ;;
  esac
  COMPREPLY=($(compgen -W '-context -s -creds -timeout -nats -mouse -json -tree -version' -- "$cur"))
}
complete -F _nats_tui nats-tui
