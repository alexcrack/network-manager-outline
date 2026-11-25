#include "outline.h"
#include "nm-outline-service.h"
#include "outlineauth.h"
#include "outlineutils.h"
#include "outlinewidget.h"

#include <KLocalizedString>
#include <KPluginFactory>
#include <QFile>

#include <NetworkManager.h>

K_PLUGIN_CLASS_WITH_JSON(OutlineUiPlugin, "plasmanetworkmanagement_outlineui.json")

OutlineUiPlugin::OutlineUiPlugin(QObject *parent, const QVariantList &)
    : VpnUiPlugin(parent)
{
}

OutlineUiPlugin::~OutlineUiPlugin() = default;

SettingWidget *OutlineUiPlugin::widget(const NetworkManager::VpnSetting::Ptr &setting, QWidget *parent)
{
    return new OutlineWidget(setting, parent);
}

SettingWidget *OutlineUiPlugin::askUser(const NetworkManager::VpnSetting::Ptr &setting, const QStringList &hints, QWidget *parent)
{
    return new OutlineAuthWidget(setting, hints, parent);
}

QString OutlineUiPlugin::suggestedFileName(const NetworkManager::ConnectionSettings::Ptr &connection) const
{
    QString name = connection->id();
    name.replace(QRegularExpression(QStringLiteral("[^A-Za-z0-9._-]+")), QStringLiteral("_"));
    return name + QStringLiteral(".txt");
}

QStringList OutlineUiPlugin::supportedFileExtensions() const
{
    // Not *.conf: OpenVPN and WireGuard claim it, and the first match wins.
    return {QStringLiteral("*.txt")};
}

VpnUiPlugin::ImportResult OutlineUiPlugin::importConnectionSettings(const QString &fileName)
{
    QFile file(fileName);
    if (!file.open(QIODevice::ReadOnly | QIODevice::Text)) {
        return ImportResult::fail(i18n("Could not open %1: %2", fileName, file.errorString()));
    }
    const QString key = OutlineUtils::findAccessKey(QString::fromUtf8(file.read(64 * 1024)));
    if (key.isEmpty()) {
        return ImportResult::fail(i18n("%1 does not contain an Outline access key (ss://… or ssconf://…)", fileName));
    }

    NMConnection *connection = nm_simple_connection_new();

    NMSettingConnection *sCon = NM_SETTING_CONNECTION(nm_setting_connection_new());
    char *uuid = nm_utils_uuid_generate();
    g_object_set(sCon,
                 NM_SETTING_CONNECTION_ID,
                 OutlineUtils::nameForKey(key).toUtf8().constData(),
                 NM_SETTING_CONNECTION_UUID,
                 uuid,
                 NM_SETTING_CONNECTION_TYPE,
                 NM_SETTING_VPN_SETTING_NAME,
                 nullptr);
    g_free(uuid);
    nm_connection_add_setting(connection, NM_SETTING(sCon));

    NMSettingVpn *sVpn = NM_SETTING_VPN(nm_setting_vpn_new());
    // Stay connected when the network changes, like a new connection in the editor.
    g_object_set(sVpn, NM_SETTING_VPN_SERVICE_TYPE, NM_DBUS_SERVICE_OUTLINE, NM_SETTING_VPN_PERSISTENT, TRUE, nullptr);
    // Same default as a new connection in the editor: stored for this user (KWallet).
    nm_setting_vpn_add_data_item(sVpn, NM_OUTLINE_SECRET_ACCESS_KEY_FLAGS, QByteArray::number(int(NM_SETTING_SECRET_FLAG_AGENT_OWNED)).constData());
    nm_setting_vpn_add_secret(sVpn, NM_OUTLINE_SECRET_ACCESS_KEY, key.toUtf8().constData());
    nm_connection_add_setting(connection, NM_SETTING(sVpn));

    GError *error = nullptr;
    if (!nm_connection_normalize(connection, nullptr, nullptr, &error)) {
        const QString message = QString::fromUtf8(error->message);
        g_error_free(error);
        g_object_unref(connection);
        return ImportResult::fail(i18n("Could not create the connection: %1", message));
    }
    return ImportResult::pass(connection);
}

#include "outline.moc"
