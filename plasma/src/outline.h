#pragma once

#include "vpnuiplugin.h"

class Q_DECL_EXPORT OutlineUiPlugin : public VpnUiPlugin
{
    Q_OBJECT
public:
    explicit OutlineUiPlugin(QObject *parent = nullptr, const QVariantList & = QVariantList());
    ~OutlineUiPlugin() override;

    SettingWidget *widget(const NetworkManager::VpnSetting::Ptr &setting, QWidget *parent) override;
    SettingWidget *askUser(const NetworkManager::VpnSetting::Ptr &setting, const QStringList &hints, QWidget *parent) override;
    QString suggestedFileName(const NetworkManager::ConnectionSettings::Ptr &connection) const override;

    // Import a text file containing an ss:// or ssconf:// access key.
    QStringList supportedFileExtensions() const override;
    ImportResult importConnectionSettings(const QString &fileName) override;
};
