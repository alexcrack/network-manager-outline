#include "outlineutils.h"

#include "passwordfield.h"

#include <QRegularExpression>
#include <QUrl>

namespace OutlineUtils
{

bool isAccessKey(const QString &text)
{
    const QString t = text.trimmed();
    return t.startsWith(QLatin1String("ss://"), Qt::CaseInsensitive) || t.startsWith(QLatin1String("ssconf://"), Qt::CaseInsensitive);
}

QString findAccessKey(const QString &text)
{
    static const QRegularExpression re(QStringLiteral("\\b(ssconf|ss)://\\S+"), QRegularExpression::CaseInsensitiveOption);
    const QRegularExpressionMatch m = re.match(text);
    return m.hasMatch() ? m.captured(0) : QString();
}

QString nameForKey(const QString &key)
{
    const QUrl url(key.trimmed());
    const QString fragment = url.fragment(QUrl::FullyDecoded).trimmed();
    if (!fragment.isEmpty()) {
        return fragment;
    }
    // SIP002 keys have a readable host; legacy all-base64 keys do not.
    if (!url.userInfo().isEmpty() && !url.host().isEmpty()) {
        return QStringLiteral("Outline (%1)").arg(url.host());
    }
    return QStringLiteral("Outline");
}

void setPasswordOption(PasswordField *field, NetworkManager::Setting::SecretFlags flags)
{
    if (flags.testFlag(NetworkManager::Setting::AgentOwned)) {
        field->setPasswordOption(PasswordField::StoreForUser);
    } else if (flags.testFlag(NetworkManager::Setting::NotSaved)) {
        field->setPasswordOption(PasswordField::AlwaysAsk);
    } else {
        field->setPasswordOption(PasswordField::StoreForAllUsers);
    }
}

NetworkManager::Setting::SecretFlags secretFlags(const PasswordField *field)
{
    switch (field->passwordOption()) {
    case PasswordField::StoreForUser:
        return NetworkManager::Setting::AgentOwned;
    case PasswordField::AlwaysAsk:
        return NetworkManager::Setting::NotSaved;
    case PasswordField::NotRequired:
        return NetworkManager::Setting::NotRequired;
    case PasswordField::StoreForAllUsers:
        break;
    }
    return NetworkManager::Setting::None;
}

} // namespace OutlineUtils
