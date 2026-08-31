module("luci.controller.taosocks", package.seeall)

function index()
	if not nixio.fs.access("/etc/config/taosocks") then
		return
	end

	entry({"admin", "services", "taosocks"}, cbi("taosocks"), _("Tao SOCKS5"), 90).dependent = true
	entry({"admin", "services", "taosocks", "status"}, call("act_status")).leaf = true
end

function act_status()
	local e = { running = (luci.sys.call("pidof taosocks >/dev/null 2>&1") == 0) }
	luci.http.prepare_content("application/json")
	luci.http.write_json(e)
end
