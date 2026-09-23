# hikidashi の zsh 補完。compinit の後に source <(hikidashi completion zsh) で読み込む。
# 候補は hikidashi __complete が「値 TAB 説明」の行で返す。候補の決め方は Go 側にあり、ここは受け渡しだけをする。
_hikidashi() {
  local -a candidates
  local line
  for line in "${(@f)$(hikidashi __complete "${(@)words[2,CURRENT]}" 2>/dev/null)}"; do
    [[ -n $line ]] || continue
    # _describe は「値:説明」を受け取るため、値の中の : をエスケープする。
    candidates+=("${${line%%$'\t'*}//:/\\:}:${line#*$'\t'}")
  done
  _describe 'hikidashi' candidates
}
compdef _hikidashi hikidashi
