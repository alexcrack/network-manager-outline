#pragma once

#include <NetworkManagerQt/Setting>
#include <QString>

class PasswordField;

namespace OutlineUtils
{
// True if text looks like an Outline access key (ss:// or ssconf://).
// The service does the full validation when connecting.
bool isAccessKey(const QString &text);

// Finds the first access key in text (one key per line, or pasted in prose).
QString findAccessKey(const QString &text);

// Display name for a key: its #fragment, else the server host, else "Outline".
QString nameForKey(const QString &key);

// Map between the PasswordField storage menu and NM secret flags.
void setPasswordOption(PasswordField *field, NetworkManager::Setting::SecretFlags flags);
NetworkManager::Setting::SecretFlags secretFlags(const PasswordField *field);
}
