#!/bin/sh
# No file → create default. File exists → keep content, only fix r/w.
# Self-contained: used as preinst before new files are unpacked.

ROOT="${IPKG_INSTROOT:-}"
DIR="${ROOT}/etc/config"
CONF="${DIR}/taosocks"

mkdir -p "$DIR" 2>/dev/null || true
chmod 755 "$DIR" 2>/dev/null || true

if [ -L "$CONF" ]; then
	rm -f "$CONF" 2>/dev/null || true
fi

if [ -f "$CONF" ] && [ -s "$CONF" ]; then
	chmod u+rw "$CONF" 2>/dev/null || true
	if [ ! -r "$CONF" ] || [ ! -w "$CONF" ]; then
		chmod 600 "$CONF" 2>/dev/null || true
	fi
	exit 0
fi

if [ -f "${ROOT}/usr/share/taosocks/taosocks.config" ]; then
	cp "${ROOT}/usr/share/taosocks/taosocks.config" "$CONF" 2>/dev/null || true
fi

if [ ! -f "$CONF" ] || [ ! -s "$CONF" ]; then
	cat > "$CONF" <<'EOF'
config taosocks 'main'
	option enabled '1'
	option auto_start '1'
	option port '28000'
	option skip_private_check '0'
	option log_flag '0'
	option core_num '0'
	option username 'admin'
	option password 'admin'
	option log_dir '/tmp/taosocks'
	option open_firewall '1'
	option firewall_src '*'
EOF
fi

chmod 600 "$CONF" 2>/dev/null || true
exit 0
