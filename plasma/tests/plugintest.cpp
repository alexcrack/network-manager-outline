// Loads the built plugin the way Plasma does and exercises it offscreen.
#include "nm-outline-service.h"
#include "passwordfield.h"
#include "vpnuiplugin.h"

#include <KPluginFactory>
#include <KPluginMetaData>
#include <NetworkManager.h>
#include <QComboBox>
#include <QCheckBox>
#include <QLineEdit>
#include <QTemporaryFile>
#include <QTest>

using namespace NetworkManager;

static const QString key = QStringLiteral("ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpzM2NyZXQ@203.0.113.7:8388/?outline=1#My%20Server");

class PluginTest : public QObject
{
    Q_OBJECT

    std::unique_ptr<VpnUiPlugin> plugin;

    static NMStringMap mapValue(const QVariantMap &m, const char *k)
    {
        return qdbus_cast<NMStringMap>(m.value(QLatin1String(k)));
    }

private Q_SLOTS:
    void initTestCase()
    {
        KPluginMetaData md(QStringLiteral(PLUGIN_PATH));
        QVERIFY2(md.isValid(), "plugin metadata not readable");
        QCOMPARE(md.value(QStringLiteral("X-NetworkManager-Services")), QStringLiteral(NM_DBUS_SERVICE_OUTLINE));
        auto result = KPluginFactory::instantiatePlugin<VpnUiPlugin>(md);
        QVERIFY2(result, qPrintable(result.errorText));
        plugin.reset(result.plugin);
    }

    void newConnectionNeedsKey()
    {
        VpnSetting::Ptr empty(new VpnSetting);
        std::unique_ptr<SettingWidget> w(plugin->widget(empty, nullptr));
        QVERIFY(!w->isValid());

        auto *field = w->findChild<PasswordField *>(QStringLiteral("accessKey"));
        QVERIFY(field);
        field->setText(QStringLiteral("not a key"));
        QVERIFY(!w->isValid());
        field->setText(key);
        QVERIFY(w->isValid());

        const QVariantMap s = w->setting();
        QCOMPARE(s.value(QStringLiteral("service-type")).toString(), QStringLiteral(NM_DBUS_SERVICE_OUTLINE));
        QVERIFY2(s.value(QStringLiteral("persistent")).toBool(), "new connections should stay connected");
        const NMStringMap secrets = mapValue(s, "secrets");
        QCOMPARE(secrets.value(QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY)), key);
        // Defaults are not stored, only the secret flags.
        QCOMPARE(mapValue(s, "data").keys(), QStringList{QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY_FLAGS)});

        w->findChild<QLineEdit *>(QStringLiteral("dns"))->setText(QStringLiteral("9.9.9.9, nonsense"));
        QVERIFY(!w->isValid());
    }

    void roundTrip()
    {
        VpnSetting::Ptr in(new VpnSetting);
        in->setServiceType(QStringLiteral(NM_DBUS_SERVICE_OUTLINE));
        const NMStringMap data{
            {QStringLiteral(NM_OUTLINE_KEY_DNS), QStringLiteral("9.9.9.9,2620:fe::fe")},
            {QStringLiteral(NM_OUTLINE_KEY_IPV6), QStringLiteral(NM_OUTLINE_IPV6_OFF)},
            {QStringLiteral(NM_OUTLINE_KEY_NEVER_DEFAULT), QStringLiteral("yes")},
            {QStringLiteral(NM_OUTLINE_KEY_TRANSPORT_PREFIX), QStringLiteral("split:2")},
            {QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY_FLAGS), QString::number(int(Setting::None))},
        };
        in->setData(data);
        in->setSecrets({{QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY), key}});

        std::unique_ptr<SettingWidget> w(plugin->widget(in, nullptr));
        QVERIFY(w->isValid());
        // An existing connection keeps its persistent value (false here).
        QVERIFY(!w->findChild<QCheckBox *>(QStringLiteral("persistent"))->isChecked());
        QVERIFY(!w->setting().value(QStringLiteral("persistent")).toBool());
        QCOMPARE(w->findChild<QComboBox *>(QStringLiteral("ipv6"))->currentIndex(), 1);
        QVERIFY(w->findChild<QCheckBox *>(QStringLiteral("neverDefault"))->isChecked());
        QCOMPARE(w->findChild<PasswordField *>(QStringLiteral("accessKey"))->passwordOption(), PasswordField::StoreForAllUsers);

        const QVariantMap out = w->setting();
        QCOMPARE(mapValue(out, "data"), data);
        QCOMPARE(mapValue(out, "secrets").value(QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY)), key);
    }

    void persistentRoundTrip()
    {
        VpnSetting::Ptr in(new VpnSetting);
        in->setData({{QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY_FLAGS), QStringLiteral("0")}});
        in->setPersistent(true);
        std::unique_ptr<SettingWidget> w(plugin->widget(in, nullptr));
        QVERIFY(w->findChild<QCheckBox *>(QStringLiteral("persistent"))->isChecked());
        w->findChild<QCheckBox *>(QStringLiteral("persistent"))->setChecked(false);
        QVERIFY(!w->setting().value(QStringLiteral("persistent")).toBool());
    }

    void alwaysAskStoresNoSecret()
    {
        VpnSetting::Ptr in(new VpnSetting);
        in->setData({{QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY_FLAGS), QString::number(int(Setting::NotSaved))}});
        std::unique_ptr<SettingWidget> w(plugin->widget(in, nullptr));
        QVERIFY(w->isValid()); // no key needed: asked on connect
        w->findChild<PasswordField *>(QStringLiteral("accessKey"))->setText(key);
        const QVariantMap out = w->setting();
        QVERIFY(mapValue(out, "secrets").isEmpty());
        QCOMPARE(mapValue(out, "data").value(QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY_FLAGS)), QString::number(int(Setting::NotSaved)));
    }

    void askUser()
    {
        VpnSetting::Ptr s(new VpnSetting);
        std::unique_ptr<SettingWidget> w(plugin->askUser(s, {}, nullptr));
        QVERIFY(!w->isValid());
        w->findChild<PasswordField *>(QStringLiteral("accessKey"))->setText(key);
        QVERIFY(w->isValid());
        QCOMPARE(mapValue(w->setting(), "secrets").value(QStringLiteral(NM_OUTLINE_SECRET_ACCESS_KEY)), key);
    }

    void importKey()
    {
        QVERIFY(plugin->supportedFileExtensions().contains(QStringLiteral("*.txt")));
        QVERIFY(!plugin->supportedFileExtensions().contains(QStringLiteral("*.conf")));

        QTemporaryFile f(QStringLiteral("XXXXXX.txt"));
        QVERIFY(f.open());
        f.write("Your Outline key:\n\n  " + key.toUtf8() + "  \n");
        f.close();

        auto result = plugin->importConnectionSettings(f.fileName());
        QVERIFY2(result, qPrintable(result.errorMessage()));
        NMConnection *c = result.connection();
        QCOMPARE(QString::fromUtf8(nm_connection_get_id(c)), QStringLiteral("My Server"));
        NMSettingVpn *vpn = nm_connection_get_setting_vpn(c);
        QVERIFY(vpn);
        QCOMPARE(QString::fromUtf8(nm_setting_vpn_get_service_type(vpn)), QStringLiteral(NM_DBUS_SERVICE_OUTLINE));
        QCOMPARE(QString::fromUtf8(nm_setting_vpn_get_secret(vpn, NM_OUTLINE_SECRET_ACCESS_KEY)), key);
        QCOMPARE(QString::fromUtf8(nm_setting_vpn_get_data_item(vpn, NM_OUTLINE_SECRET_ACCESS_KEY_FLAGS)), QStringLiteral("1"));
        QVERIFY(nm_setting_vpn_get_persistent(vpn));
        QVERIFY(nm_connection_verify(c, nullptr));
        g_object_unref(c);
    }

    void importDynamicKey_data()
    {
        QTest::addColumn<QString>("key");
        QTest::addColumn<QString>("name");
        QTest::newRow("fragment name") << QStringLiteral("ssconf://keys.example.com/u/123#Work%20VPN") << QStringLiteral("Work VPN");
        QTest::newRow("name among key=value") << QStringLiteral("ssconf://keys.example.com/u/123#a=1&Home&b=2") << QStringLiteral("Home");
        QTest::newRow("no name") << QStringLiteral("ssconf://keys.example.com/u/123") << QStringLiteral("Outline (keys.example.com)");
    }

    void importDynamicKey()
    {
        QFETCH(QString, key);
        QFETCH(QString, name);
        QTemporaryFile f(QStringLiteral("XXXXXX.txt"));
        QVERIFY(f.open());
        f.write(key.toUtf8() + "\n");
        f.close();
        auto result = plugin->importConnectionSettings(f.fileName());
        QVERIFY2(result, qPrintable(result.errorMessage()));
        QCOMPARE(QString::fromUtf8(nm_connection_get_id(result.connection())), name);
        QCOMPARE(QString::fromUtf8(nm_setting_vpn_get_secret(nm_connection_get_setting_vpn(result.connection()), NM_OUTLINE_SECRET_ACCESS_KEY)), key);
        g_object_unref(result.connection());
    }

    void dynamicKeyIsValid()
    {
        VpnSetting::Ptr empty(new VpnSetting);
        std::unique_ptr<SettingWidget> w(plugin->widget(empty, nullptr));
        auto *field = w->findChild<PasswordField *>(QStringLiteral("accessKey"));
        field->setText(QStringLiteral("ssconf://keys.example.com/u/123"));
        QVERIFY(w->isValid());
        field->setText(QStringLiteral("https://keys.example.com/u/123"));
        QVERIFY(w->isValid());
    }

    void importRejectsOtherFiles()
    {
        QTemporaryFile f(QStringLiteral("XXXXXX.txt"));
        QVERIFY(f.open());
        f.write("just some notes\n");
        f.close();
        auto result = plugin->importConnectionSettings(f.fileName());
        QVERIFY(!result);
        QVERIFY(result.errorMessage().contains(QStringLiteral("access key")));
    }
};

QTEST_MAIN(PluginTest)
#include "plugintest.moc"
