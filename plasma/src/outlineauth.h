#pragma once

#include "settingwidget.h"

#include <NetworkManagerQt/VpnSetting>

namespace Ui
{
class OutlineAuth;
}

// Asks for the access key when it is not stored (Plasma's secret agent).
class OutlineAuthWidget : public SettingWidget
{
    Q_OBJECT
public:
    explicit OutlineAuthWidget(const NetworkManager::VpnSetting::Ptr &setting, const QStringList &hints, QWidget *parent = nullptr);
    ~OutlineAuthWidget() override;

    QVariantMap setting() const override;
    bool isValid() const override;

private:
    Ui::OutlineAuth *const m_ui;
    NetworkManager::VpnSetting::Ptr m_setting;
};
