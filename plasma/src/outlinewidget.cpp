#include "outlinewidget.h"
#include "nm-outline-service.h"
#include "outlineutils.h"
#include "passwordfield.h"
#include "ui_outlinewidget.h"

#include <QHostAddress>
#include <QRegularExpression>

static QStringList splitDns(const QString &text)
{
    static const QRegularExpression sep(QStringLiteral("[,;\\s]+"));
    return text.split(sep, Qt::SkipEmptyParts);
}

OutlineWidget::OutlineWidget(const NetworkManager::VpnSetting::Ptr &setting, QWidget *parent)
    : SettingWidget(setting, parent)
    , m_ui(new Ui::OutlineWidget)
    , m_setting(setting)
{
    m_ui->setupUi(this);

    m_ui->accessKey->setPasswordModeEnabled(true);
    m_ui->accessKey->setPasswordOptionsEnabled(true);
    m_ui->accessKey->setPasswordNotRequiredEnabled(false); // the key is always required

    connect(m_ui->accessKey, &PasswordField::textChanged, this, &OutlineWidget::slotWidgetChanged);
    connect(m_ui->accessKey, &PasswordField::passwordOptionChanged, this, &OutlineWidget::slotWidgetChanged);
    connect(m_ui->dns, &QLineEdit::textChanged, this, &OutlineWidget::slotWidgetChanged);
    connect(m_ui->persistent, &QCheckBox::toggled, this, &OutlineWidget::slotWidgetChanged);

    KAcceleratorManager::manage(this);

    // Load whenever a setting is given: a VpnSetting built in code reports
    // isNull() until marked initialized, and an empty one loads as defaults.
    if (m_setting) {
        loadConfig(m_setting);
    }

    watchChangedSetting();
}

OutlineWidget::~OutlineWidget()
{
    delete m_ui;
}

void OutlineWidget::loadConfig(const NetworkManager::Setting::Ptr &setting)
{
    Q_UNUSED(setting)
    const NMStringMap data = m_setting->data();

    // A new connection has no data yet: stay connected by default.
    m_ui->persistent->setChecked(data.isEmpty() || m_setting->persistent());

    const auto flags = static_cast<NetworkManager::Setting::SecretFlags>(data.value(QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY_FLAGS)).toInt());
    OutlineUtils::setPasswordOption(m_ui->accessKey, flags);

    m_ui->dns->setText(splitDns(data.value(QStringLiteral(NM_OUTLINE_KEY_DNS))).join(QStringLiteral(", ")));
    m_ui->ipv6->setCurrentIndex(data.value(QStringLiteral(NM_OUTLINE_KEY_IPV6)) == QLatin1String(NM_OUTLINE_IPV6_OFF) ? 1 : 0);
    m_ui->neverDefault->setChecked(data.value(QStringLiteral(NM_OUTLINE_KEY_NEVER_DEFAULT)) == QLatin1String("yes"));
    m_ui->transportPrefix->setText(data.value(QStringLiteral(NM_OUTLINE_KEY_TRANSPORT_PREFIX)));

    loadSecrets(m_setting);
}

void OutlineWidget::loadSecrets(const NetworkManager::Setting::Ptr &setting)
{
    const auto vpn = setting.staticCast<NetworkManager::VpnSetting>();
    if (!vpn) {
        return;
    }
    const QString key = vpn->secrets().value(QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY));
    if (!key.isEmpty()) {
        m_ui->accessKey->setText(key);
    }
}

QVariantMap OutlineWidget::setting() const
{
    NetworkManager::VpnSetting vpn;
    vpn.setServiceType(QStringLiteral(NM_DBUS_SERVICE_OUTLINE));
    vpn.setPersistent(m_ui->persistent->isChecked());

    NMStringMap data;
    NMStringMap secrets;

    const auto flags = OutlineUtils::secretFlags(m_ui->accessKey);
    data.insert(QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY_FLAGS), QString::number(int(flags)));
    const QString key = m_ui->accessKey->text().trimmed();
    if (!key.isEmpty() && !flags.testFlag(NetworkManager::Setting::NotSaved)) {
        secrets.insert(QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY), key);
    }

    // Only non-default values are stored; the service applies the defaults.
    const QStringList dns = splitDns(m_ui->dns->text());
    if (!dns.isEmpty()) {
        data.insert(QStringLiteral(NM_OUTLINE_KEY_DNS), dns.join(QLatin1Char(',')));
    }
    if (m_ui->ipv6->currentIndex() == 1) {
        data.insert(QStringLiteral(NM_OUTLINE_KEY_IPV6), QStringLiteral(NM_OUTLINE_IPV6_OFF));
    }
    if (m_ui->neverDefault->isChecked()) {
        data.insert(QStringLiteral(NM_OUTLINE_KEY_NEVER_DEFAULT), QStringLiteral("yes"));
    }
    const QString prefix = m_ui->transportPrefix->text().trimmed();
    if (!prefix.isEmpty()) {
        data.insert(QStringLiteral(NM_OUTLINE_KEY_TRANSPORT_PREFIX), prefix);
    }

    vpn.setData(data);
    vpn.setSecrets(secrets);
    return vpn.toMap();
}

bool OutlineWidget::dnsValid() const
{
    const QStringList dns = splitDns(m_ui->dns->text());
    return std::all_of(dns.cbegin(), dns.cend(), [](const QString &s) {
        return !QHostAddress(s).isNull();
    });
}

bool OutlineWidget::isValid() const
{
    if (!dnsValid()) {
        return false;
    }
    if (m_ui->accessKey->passwordOption() == PasswordField::AlwaysAsk) {
        return true; // asked for on every connect
    }
    return OutlineUtils::isAccessKey(m_ui->accessKey->text());
}

#include "moc_outlinewidget.cpp"
