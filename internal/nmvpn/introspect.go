package nmvpn

import (
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// pluginInterfaceXML follows introspection/org.freedesktop.NetworkManager.VPN.Plugin.xml (NM 1.54.3).
const pluginInterfaceXML = `
	<interface name="` + PluginInterface + `">
		<method name="Connect">
			<arg name="connection" type="a{sa{sv}}" direction="in"/>
		</method>
		<method name="ConnectInteractive">
			<arg name="connection" type="a{sa{sv}}" direction="in"/>
			<arg name="details" type="a{sv}" direction="in"/>
		</method>
		<method name="NeedSecrets">
			<arg name="settings" type="a{sa{sv}}" direction="in"/>
			<arg name="setting_name" type="s" direction="out"/>
		</method>
		<method name="Disconnect"/>
		<method name="SetConfig">
			<arg name="config" type="a{sv}" direction="in"/>
		</method>
		<method name="SetIp4Config">
			<arg name="config" type="a{sv}" direction="in"/>
		</method>
		<method name="SetIp6Config">
			<arg name="config" type="a{sv}" direction="in"/>
		</method>
		<method name="SetFailure">
			<arg name="reason" type="s" direction="in"/>
		</method>
		<method name="NewSecrets">
			<arg name="connection" type="a{sa{sv}}" direction="in"/>
		</method>
		<property name="State" type="u" access="read"/>
		<signal name="StateChanged">
			<arg name="state" type="u"/>
		</signal>
		<signal name="SecretsRequired">
			<arg name="message" type="s"/>
			<arg name="secrets" type="as"/>
		</signal>
		<signal name="Config">
			<arg name="config" type="a{sv}"/>
		</signal>
		<signal name="Ip4Config">
			<arg name="ip4config" type="a{sv}"/>
		</signal>
		<signal name="Ip6Config">
			<arg name="ip6config" type="a{sv}"/>
		</signal>
		<signal name="LoginBanner">
			<arg name="banner" type="s"/>
		</signal>
		<signal name="Failure">
			<arg name="reason" type="u"/>
		</signal>
	</interface>`

const introspectXML = `<node>` +
	introspect.IntrospectDataString +
	prop.IntrospectDataString +
	pluginInterfaceXML +
	`</node>`
