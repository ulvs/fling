#pragma once

namespace Fling::Core
{
    // Installs/uninstalls a "Fling" shortcut in the user's SendTo folder
    // (%APPDATA%\Microsoft\Windows\SendTo\). When enabled, right-clicking any
    // file in Explorer → Send to → Fling invokes Fling.exe with the selected
    // file as its first command-line argument.
    //
    // Unlike classic shell verbs under HKCU\Software\Classes\*\shell, SendTo
    // entries are reliably shown by Windows 11's new context menu and cost
    // nothing on unrelated right-clicks.
    class SendToInstaller
    {
    public:
        static bool Install();
        static bool Uninstall();
        static bool IsInstalled();
    };
}
