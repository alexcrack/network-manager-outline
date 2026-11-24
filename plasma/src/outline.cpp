#include "outline.h"
#include "outlineauth.h"
#include "outlinewidget.h"

#include <KPluginFactory>

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
    return connection->id() + QStringLiteral("_outline.txt");
}

QStringList OutlineUiPlugin::supportedFileExtensions() const
{
    return {QStringLiteral("*.txt"), QStringLiteral("*.conf")};
}

VpnUiPlugin::ImportResult OutlineUiPlugin::importConnectionSettings(const QString &fileName)
{
    // TODO(phase3): read the ss:// / ssconf:// key, build an NMConnection with
    // NMSettingVpn (service-type, data, secrets) and return ImportResult::pass().
    Q_UNUSED(fileName)
    return ImportResult::notImplemented();
}

#include "outline.moc"
