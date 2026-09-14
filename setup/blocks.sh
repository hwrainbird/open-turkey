#!/usr/bin/env bash
#
# Cria os blocos do Open Turkey a partir dos arquivos em setup/lists/.
#
# Rode DEPOIS de instalar (sudo ./install.sh) e ANTES de abrir mão do acesso
# de administrador. Editar um bloco travado custa o desafio de digitação;
# montar tudo agora é de graça.
#
#   sudo ./setup/blocks.sh            # cria os blocos, sem ativar nada
#   sudo ./setup/blocks.sh --activate # cria e já agenda/liga
#
# O script é idempotente na parte de sites: rodar duas vezes não duplica
# domínios (o Open Turkey aceita a inclusão repetida sem reclamar), mas
# "block create" falha no segundo uso — por isso ignoramos esse erro.

set -euo pipefail

AQUI="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LISTAS="$AQUI/lists"
OT="${OT:-open-turkey}"
ATIVAR="${1:-}"

# OT permite apontar para outro binário — usado para testar o script sem root
# e sem tocar no banco de verdade. Só exigimos root quando estamos de fato
# falando com o open-turkey instalado.
if [ "$OT" = "open-turkey" ] && [ "$(id -u)" -ne 0 ]; then
    echo "Erro: rode como root (sudo ./setup/blocks.sh)." >&2
    exit 1
fi

if ! command -v "$OT" >/dev/null 2>&1; then
    echo "Erro: '$OT' não encontrado no PATH. Instale primeiro com sudo ./install.sh" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# Funções auxiliares
# ---------------------------------------------------------------------------

# dominios <arquivo> — imprime as linhas úteis: sem comentários, sem linhas
# vazias, sem espaços nas pontas e sem o \r que vem de listas exportadas no
# Windows. Esse \r é invisível no editor e transformaria cada domínio em algo
# que o Open Turkey recusa.
dominios() {
    tr -d '\r' < "$1" | sed 's/#.*//; s/[[:space:]]*$//' | grep -v '^$' || true
}

criar_bloco() {
    local nome="$1"; shift
    if "$OT" block info "$nome" >/dev/null 2>&1; then
        echo "  bloco '$nome' já existe — mantendo"
    else
        "$OT" block create "$nome" "$@"
        echo "  bloco '$nome' criado"
    fi
}

# Os domínios entram em lotes para não estourar o limite de argumentos da linha
# de comando — mil e poucos domínios de uma vez só passariam do limite em
# muitas distribuições.
adicionar_sites() {
    local bloco="$1" arquivo="$2" total=0
    local -a lote

    while read -r -a lote; do
        [ ${#lote[@]} -eq 0 ] && continue
        "$OT" block add-site "$bloco" "${lote[@]}"
        total=$(( total + ${#lote[@]} ))
    done < <(dominios "$arquivo" | xargs -n 50 echo)

    echo "  $total domínios em '$bloco'"
}

# ---------------------------------------------------------------------------
# 1. ai — bloqueado o tempo todo, com cinco minutos de folga no sábado
# ---------------------------------------------------------------------------
#
# api.anthropic.com NÃO está na lista, de propósito: o Claude Code fala com a
# API, não com claude.ai. O site sai do ar, o terminal continua funcionando.
#
# github.com também ficou de fora. A lista original tinha regras específicas de
# página (adobe.com/products/firefly e afins); extraídas sem o caminho, elas
# viravam bloqueio do domínio inteiro e derrubariam git, gh e o download de
# módulos Go junto.

echo "==> AI"
criar_bloco ai
adicionar_sites ai "$LISTAS/ai.txt"

# ---------------------------------------------------------------------------
# 2. attention — YouTube, Reddit e notícias, sempre
# ---------------------------------------------------------------------------

echo "==> Attention (YouTube, Reddit, news)"
criar_bloco attention
adicionar_sites attention "$LISTAS/attention.txt"

# ---------------------------------------------------------------------------
# 3. vice — pornografia, apostas e jogos, sempre
# ---------------------------------------------------------------------------
#
# As 22 regras de palavra-chave da lista original (*p=*betting* e afins) não
# têm equivalente aqui: o Open Turkey bloqueia domínios, não pedaços de URL.
# Quem cobre essa brecha é o bloco "evening", que a partir das 17h30 bloqueia
# tudo que não estiver na lista-branca.

echo "==> Vice (porn, gambling, games)"
criar_bloco vice
adicionar_sites vice "$LISTAS/vice.txt"

# ---------------------------------------------------------------------------
# 4. evening — lista-branca a partir das 17h30
# ---------------------------------------------------------------------------
#
# Este bloco é ao contrário dos outros: o que está na lista é o que PASSA.
# Vale só dentro do navegador — terminal, ssh, git e atualizações do sistema
# não são afetados.

echo "==> Evening (whitelist)"
criar_bloco evening --allow
adicionar_sites evening "$LISTAS/allowlist.txt"

# ---------------------------------------------------------------------------
# Agenda
# ---------------------------------------------------------------------------

if [ "$ATIVAR" = "--activate" ]; then
    echo
    echo "==> Agendando"

    # ai: coberto 24h por dia, menos sábado das 10:00 às 10:05.
    "$OT" block schedule-except ai sat 10:00-10:05

    # attention e vice: o tempo todo, todos os dias.
    "$OT" block schedule attention daily 00:00-24:00
    "$OT" block schedule vice daily 00:00-24:00

    # evening: dias de semana e domingo, a partir das 17h30.
    "$OT" block schedule evening mon-fri 17:30-24:00
    "$OT" block schedule evening sun 17:30-24:00

    echo
    echo "Agendas aplicadas. O daemon liga e desliga sozinho a partir de agora."
else
    echo
    echo "Blocos criados, nada ativado ainda."
    echo "Confira com 'open-turkey block info <nome>' e, quando estiver satisfeito:"
    echo "  sudo ./setup/blocks.sh --activate"
fi
