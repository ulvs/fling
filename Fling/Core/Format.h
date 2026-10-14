#pragma once
#include <cstdint>
#include <iomanip>
#include <sstream>
#include <string>

namespace Fling::Core
{
    inline std::wstring FormatSize(uint64_t bytes)
    {
        constexpr double kb = 1024.0;
        constexpr double mb = kb * 1024.0;
        constexpr double gb = mb * 1024.0;

        std::wostringstream os;
        os << std::fixed << std::setprecision(1);
        if (bytes >= gb)       os << (bytes / gb) << L" GB";
        else if (bytes >= mb)  os << (bytes / mb) << L" MB";
        else if (bytes >= kb)  os << (bytes / kb) << L" KB";
        else                   os << bytes << L" B";
        return os.str();
    }
}
