local sys = require "luci.sys"

m = Map("taosocks", translate("Tao SOCKS5"),
	translate("SOCKS5 代理服务。点「保存并应用」后会立即重启进程，无需重启路由器。外网访问请先改密码。"))

m.apply_on_parse = false

s = m:section(NamedSection, "main", "taosocks", translate("服务状态"))
s.addremove = false
s.anonymous = false

o = s:option(DummyValue, "_status", translate("运行状态"))
o.rawhtml = true
function o.cfgvalue()
	if sys.call("pidof taosocks >/dev/null 2>&1") == 0 then
		return "<span style=\"color:green;font-weight:bold\">" .. translate("运行中") .. "</span>"
	end
	return "<span style=\"color:red;font-weight:bold\">" .. translate("未运行") .. "</span>"
end

btn = s:option(Button, "_restart", translate("重启服务"))
btn.inputtitle = translate("立即重启")
btn.inputstyle = "apply"
function btn.write()
	sys.call("/etc/init.d/taosocks restart >/dev/null 2>&1")
end

s = m:section(NamedSection, "main", "taosocks", translate("基本设置"))
s.addremove = false
s.anonymous = false

o = s:option(Flag, "enabled", translate("启用服务"))
o.rmempty = false
o.default = "1"

o = s:option(Flag, "auto_start", translate("开机自启"))
o.rmempty = false
o.default = "1"

o = s:option(Value, "port", translate("监听端口"))
o.datatype = "port"
o.default = "28000"
o.rmempty = false

o = s:option(Value, "username", translate("用户名"))
o.default = "admin"
o.rmempty = false

o = s:option(Value, "password", translate("密码"))
o.password = true
o.default = "admin"
o.rmempty = false

o = s:option(Flag, "skip_private_check", translate("内网免认证"))
o.description = translate("来源地址为 10.10.* 的客户端跳过用户名和密码")
o.default = "0"

o = s:option(Value, "core_num", translate("工作线程数"))
o.datatype = "uinteger"
o.default = "0"
o.description = translate("0 表示 CPU 核数 × 2")

s = m:section(NamedSection, "main", "taosocks", translate("日志"))
s.addremove = false

o = s:option(Flag, "log_flag", translate("启用文件日志"))
o.default = "0"

o = s:option(Value, "log_dir", translate("日志目录"))
o.default = "/tmp/taosocks"
o:depends("log_flag", "1")

s = m:section(NamedSection, "main", "taosocks", translate("防火墙"))
s.addremove = false

o = s:option(Flag, "open_firewall", translate("自动放行端口"))
o.default = "1"
o.description = translate("写入防火墙规则。默认所有区域，局域网和外网都能连。")
o.rmempty = false

o = s:option(ListValue, "firewall_src", translate("放行区域"))
o:value("*", translate("所有区域"))
o:value("wan", "wan")
o:value("lan", "lan")
o.default = "*"
o:depends("open_firewall", "1")

local function restart_now()
	sys.call("/etc/init.d/taosocks restart >/dev/null 2>&1")
end

function m.on_after_commit(map)
	restart_now()
end

function m.on_after_apply(map)
	restart_now()
end

return m
