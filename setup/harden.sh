#!/usr/bin/env bash
#
# Guia para tirar o acesso de administrador do usuário do dia a dia.
#
# === POR QUE ISTO EXISTE ===
#
# Enquanto o seu usuário tiver sudo, o Open Turkey está a um comando de ser
# desligado. "systemctl stop open-turkey" e acabou — e essa informação não é
# segredo de ninguém: está em qualquer manual, e qualquer assistente de IA
# responde na hora.
#
# Esconder o método nunca foi uma defesa. A fricção precisa vir de uma
# CAPACIDADE que você não tem às onze da noite, não de um conhecimento que lhe
# falta. Por isso o caminho é abrir mão do sudo e guardar a credencial longe.
#
# === O TEXTO NA TELA ESTÁ EM INGLÊS DE PROPÓSITO ===
#
# Os comentários seguem em português, como o resto do projeto. As perguntas
# feitas ao usuário, não: este script pode deixar alguém sem administrador na
# própria máquina, e ninguém deve concordar com isso em um idioma que não lê
# com conforto.

set -euo pipefail

ADMIN="${ADMIN:-otadmin}"
BINARIO="/usr/local/bin/open-turkey-bin"
SUDOERS_OT="/etc/sudoers.d/open-turkey"

vermelho() { printf '\033[31m%s\033[0m\n' "$*"; }
verde()    { printf '\033[32m%s\033[0m\n' "$*"; }
amarelo()  { printf '\033[33m%s\033[0m\n' "$*"; }
titulo()   { printf '\n\033[1m== %s ==\033[0m\n\n' "$*"; }

# perguntar <texto> — só segue com um "yes" escrito por extenso.
#
# "y" é rápido demais para uma decisão que pode custar o administrador da
# máquina. Exigir a palavra inteira é o mesmo princípio do desafio de digitação.
perguntar() {
    local resposta
    printf '%s\n' "$1"
    printf "Type 'yes' to continue, anything else to stop: "
    read -r resposta
    [ "$resposta" = "yes" ]
}

# ---------------------------------------------------------------------------
# Diagnóstico — não muda nada
# ---------------------------------------------------------------------------

diagnostico() {
    local alvo="$1"

    titulo "Current state"

    printf 'Daily user:        %s\n' "$alvo"

    if id -nG "$alvo" 2>/dev/null | tr ' ' '\n' | grep -qx wheel; then
        vermelho "In 'wheel' group:  YES  — this user can become root"
    else
        verde     "In 'wheel' group:  no   — this user cannot become root"
    fi

    if id "$ADMIN" >/dev/null 2>&1; then
        if id -nG "$ADMIN" 2>/dev/null | tr ' ' '\n' | grep -qx wheel; then
            verde "Admin account:     '$ADMIN' exists and is in wheel"
        else
            amarelo "Admin account:     '$ADMIN' exists but is NOT in wheel — it cannot administer"
        fi
    else
        amarelo "Admin account:     none ('$ADMIN' does not exist yet)"
    fi

    # Sem uma regra para o grupo wheel, estar no wheel não dá sudo nenhum —
    # e a conta de administrador nova nasceria inútil.
    if grep -rqE '^[[:space:]]*%wheel[[:space:]]' /etc/sudoers /etc/sudoers.d/ 2>/dev/null; then
        verde "wheel has sudo:    yes"
    else
        vermelho "wheel has sudo:    NO — being in wheel would grant nothing"
    fi

    # Esta é a brecha que interessa: qualquer NOPASSWD que não seja o do
    # próprio open-turkey é um caminho de volta ao root sem senha.
    local brechas
    brechas="$(grep -rlE 'NOPASSWD' /etc/sudoers /etc/sudoers.d/ 2>/dev/null | grep -v "^$SUDOERS_OT$" || true)"
    if [ -n "$brechas" ]; then
        vermelho "Passwordless sudo: FOUND outside open-turkey's own rule:"
        printf '                   %s\n' $brechas
        printf '                   Omarchy ships one of these by default. It has to go,\n'
        printf '                   or handing in your admin rights changes nothing.\n'
    else
        verde "Passwordless sudo: only open-turkey's own rule"
    fi

    if [ -f "$SUDOERS_OT" ]; then
        printf '\nopen-turkey sudoers rule:\n'
        sed 's/^/  /' "$SUDOERS_OT"
        printf '  (this one is wanted: it lets you run the blocker without a password,\n'
        printf '   and the blocker refuses to unlock without the typing challenge)\n'
    fi
}

# ---------------------------------------------------------------------------
# Aplicação
# ---------------------------------------------------------------------------

aplicar() {
    local alvo="$1"

    titulo "What this will do"

    cat <<'TEXTO'
  1. Create a second account that holds the admin rights.
  2. Give it a long random password that you will never see twice.
  3. Make you prove you wrote the password down, by logging in as that account.
  4. Only then, remove your everyday user from the admin group.

After this, your daily user cannot run sudo. Every system update and every
package install needs the new password. That is the cost, and it is the whole
point: the blocker becomes something you cannot switch off on impulse.

If you lose the password AND have no other admin account, your way back in is
a live USB. Write it down on paper before step 3, and keep a second copy
somewhere different.
TEXTO

    perguntar "" || { echo "Stopped. Nothing changed."; exit 0; }

    # --- Conta de administrador ---
    titulo "Step 1 — admin account"

    if id "$ADMIN" >/dev/null 2>&1; then
        echo "Account '$ADMIN' already exists; leaving it alone."
    else
        useradd -m -G wheel "$ADMIN"
        verde "Created '$ADMIN' and added it to wheel."
    fi

    # 24 caracteres de /dev/urandom. Não é para ser memorizada — é para ser
    # escrita num papel e guardada longe. Por isso é longa e sem sentido.
    local senha
    senha="$(tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 24)"
    printf '%s:%s\n' "$ADMIN" "$senha" | chpasswd

    titulo "Step 2 — write this down, on paper, now"

    printf '\n    Username: %s\n' "$ADMIN"
    printf '    Password: %s\n\n' "$senha"

    amarelo "This is shown once. It is not saved anywhere."
    echo "Put it somewhere that costs you a walk: the garage, a sealed envelope,"
    echo "another building. The distance is what does the work."
    echo

    perguntar "Have you written it down?" || {
        vermelho "Stopped. The account exists with that password, but your rights are untouched."
        exit 0
    }

    # --- Verificação ---
    #
    # Este passo é o que separa um plano de um desastre. Rodamos o "su" COMO O
    # USUÁRIO COMUM, e não como root: root troca de usuário sem senha nenhuma,
    # o que não provaria nada.
    titulo "Step 3 — prove it works"

    echo "Log in as '$ADMIN' using the password you just wrote down."
    echo "If this fails, nothing is removed and you can start again."
    echo

    if ! runuser -u "$alvo" -- su - "$ADMIN" -c 'true'; then
        vermelho "Could not log in as '$ADMIN'. Nothing was removed — you still have admin."
        echo "Check the password you wrote down and run this again."
        exit 1
    fi
    verde "Login confirmed."

    # --- Remoção dos direitos ---
    titulo "Step 4 — remove your admin rights"

    # Nunca deixar a máquina sem nenhum administrador.
    local membros
    membros="$(getent group wheel | cut -d: -f4)"
    if [ "$membros" = "$alvo" ] && ! id -nG "$ADMIN" | tr ' ' '\n' | grep -qx wheel; then
        vermelho "Refusing: '$alvo' would be the last admin and '$ADMIN' is not in wheel."
        exit 1
    fi

    perguntar "Remove '$alvo' from the wheel group? This is the irreversible half." || {
        echo "Stopped. '$ADMIN' is ready whenever you are."
        exit 0
    }

    gpasswd -d "$alvo" wheel
    verde "Done. '$alvo' is no longer an administrator."

    titulo "Where that leaves you"

    cat <<TEXTO
  Blocker commands      still work, no password (the sudoers rule stays)
  Unlocking a block     still just the typing challenge
  System updates        need the '$ADMIN' password
  Editing your dotfiles unaffected — that all lives in your home folder

Recovery, if you ever need it: log in as '$ADMIN' and run
  sudo gpasswd -a $alvo wheel

Log out and back in for the group change to take effect everywhere.
TEXTO
}

# ---------------------------------------------------------------------------
# Entrada
# ---------------------------------------------------------------------------

# ALVO_OVERRIDE existe porque SUDO_USER some em alguns contextos (um shell de
# root aberto direto, por exemplo) e nesses casos é preciso dizer quem é o
# usuário do dia a dia.
ALVO="${ALVO_OVERRIDE:-${SUDO_USER:-${USER:-}}}"
MODO="${1:---check}"

if [ "$MODO" = "--apply" ] && [ "$(id -u)" -ne 0 ]; then
    echo "Erro: --apply precisa de root (sudo ./setup/harden.sh --apply)." >&2
    exit 1
fi

if [ -z "$ALVO" ] || [ "$ALVO" = "root" ]; then
    echo "Erro: não consegui identificar o seu usuário comum." >&2
    echo "Rode via sudo a partir da sua conta, ou informe: ALVO_OVERRIDE=seuusuario $0 $MODO" >&2
    exit 1
fi
case "$MODO" in
    --check)
        diagnostico "$ALVO"
        printf '\nNothing was changed. To go ahead: sudo ./setup/harden.sh --apply\n'
        ;;
    --apply)
        if [ ! -t 0 ]; then
            echo "Erro: --apply precisa de um terminal de verdade (há perguntas a responder)." >&2
            exit 1
        fi
        diagnostico "$ALVO"
        aplicar "$ALVO"
        ;;
    *)
        echo "Uso: $0 [--check|--apply]" >&2
        exit 1
        ;;
esac
