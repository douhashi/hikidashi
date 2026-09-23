# hikidashi の bash 補完。source <(hikidashi completion bash) で読み込む。bash-completion は要らない。
# 候補は hikidashi __complete が「値 TAB 説明」の行で返す。候補の決め方は Go 側にあり、ここは受け渡しだけをする。
_hikidashi() {
  local line value
  COMPREPLY=()
  while IFS= read -r line; do
    # 空白等を含む値も 1 語として入るよう、シェルの引用にしてから候補にする。
    printf -v value '%q' "${line%%$'\t'*}"
    COMPREPLY+=("$value")
  done < <(hikidashi __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null)
}
complete -F _hikidashi hikidashi
