#include "outlineauth.h"
#include "nm-outline-service.h"
#include "ui_outlineauth.h"

OutlineAuthWidget::OutlineAuthWidget(const NetworkManager::VpnSetting::Ptr &setting, const QStringList &hints, QWidget *parent)
    : SettingWidget(setting, hints, parent)
    , m_ui(new Ui::OutlineAuth)
    , m_setting(setting)
{
    m_ui->setupUi(this);
}

OutlineAuthWidget::~OutlineAuthWidget()
{
    delete m_ui;
}

QVariantMap OutlineAuthWidget::setting() const
{
    // TODO(phase3): return {secrets: {access-key: ...}} as NMStringMap.
    return {};
}
