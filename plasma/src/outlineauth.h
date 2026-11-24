#pragma once

#include "settingwidget.h"

#include <NetworkManagerQt/VpnSetting>

namespace Ui
{
class OutlineAuth;
}

// Shown by Plasma's secret agent when the access key is not stored.
class OutlineAuthWidget : public SettingWidget
{
    Q_OBJECT
public:
    explicit OutlineAuthWidget(const NetworkManager::VpnSetting::Ptr &setting, const QStringList &hints, QWidget *parent = nullptr);
    ~OutlineAuthWidget() override;

    QVariantMap setting() const override;

private:
    Ui::OutlineAuth *const m_ui;
    NetworkManager::VpnSetting::Ptr m_setting;
};
