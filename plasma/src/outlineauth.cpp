#include "outlineauth.h"
#include "nm-outline-service.h"
#include "outlineutils.h"
#include "passwordfield.h"
#include "ui_outlineauth.h"

OutlineAuthWidget::OutlineAuthWidget(const NetworkManager::VpnSetting::Ptr &setting, const QStringList &hints, QWidget *parent)
    : SettingWidget(setting, hints, parent)
    , m_ui(new Ui::OutlineAuth)
    , m_setting(setting)
{
    m_ui->setupUi(this);
    m_ui->accessKey->setPasswordModeEnabled(true);
    connect(m_ui->accessKey, &PasswordField::textChanged, this, &OutlineAuthWidget::slotWidgetChanged);
    KAcceleratorManager::manage(this);
}

OutlineAuthWidget::~OutlineAuthWidget()
{
    delete m_ui;
}

QVariantMap OutlineAuthWidget::setting() const
{
    NMStringMap secrets;
    const QString key = m_ui->accessKey->text().trimmed();
    if (!key.isEmpty()) {
        secrets.insert(QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY), key);
    }
    QVariantMap result;
    result.insert(QStringLiteral("secrets"), QVariant::fromValue<NMStringMap>(secrets));
    return result;
}

bool OutlineAuthWidget::isValid() const
{
    return OutlineUtils::isAccessKey(m_ui->accessKey->text());
}

#include "moc_outlineauth.cpp"
