#include "outlinewidget.h"
#include "nm-outline-service.h"
#include "ui_outlinewidget.h"

OutlineWidget::OutlineWidget(const NetworkManager::VpnSetting::Ptr &setting, QWidget *parent)
    : SettingWidget(setting, parent)
    , m_ui(new Ui::OutlineWidget)
    , m_setting(setting)
{
    m_ui->setupUi(this);
    m_ui->accessKey->setPasswordModeEnabled(true);

    connect(m_ui->accessKey, &PasswordField::textChanged, this, &OutlineWidget::slotWidgetChanged);
    watchChangedSetting();

    if (setting) {
        loadConfig(setting);
    }
}

OutlineWidget::~OutlineWidget()
{
    delete m_ui;
}

void OutlineWidget::loadConfig(const NetworkManager::Setting::Ptr &setting)
{
    // TODO(phase3): populate DNS / IPv6 mode / never-default / transport prefix
    // from vpn.data and the access-key secret flags.
    Q_UNUSED(setting)
    loadSecrets(m_setting);
}

void OutlineWidget::loadSecrets(const NetworkManager::Setting::Ptr &setting)
{
    // TODO(phase3): m_ui->accessKey->setText(secrets[NM_OUTLINE_SECRET_ACCESS_KEY]).
    Q_UNUSED(setting)
}

QVariantMap OutlineWidget::setting() const
{
    // TODO(phase3): build NetworkManager::VpnSetting with service type
    // NM_DBUS_SERVICE_OUTLINE, data and secrets, then return toMap().
    return {};
}

bool OutlineWidget::isValid() const
{
    const QString key = m_ui->accessKey->text().trimmed();
    return key.startsWith(QLatin1String("ss://")) || key.startsWith(QLatin1String("ssconf://"));
}
