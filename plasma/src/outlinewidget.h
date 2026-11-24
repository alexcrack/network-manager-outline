#pragma once

#include "settingwidget.h"

#include <NetworkManagerQt/VpnSetting>

namespace Ui
{
class OutlineWidget;
}

class OutlineWidget : public SettingWidget
{
    Q_OBJECT
public:
    explicit OutlineWidget(const NetworkManager::VpnSetting::Ptr &setting, QWidget *parent = nullptr);
    ~OutlineWidget() override;

    void loadConfig(const NetworkManager::Setting::Ptr &setting) override;
    void loadSecrets(const NetworkManager::Setting::Ptr &setting) override;
    QVariantMap setting() const override;
    bool isValid() const override;

private:
    Ui::OutlineWidget *const m_ui;
    NetworkManager::VpnSetting::Ptr m_setting;
};
