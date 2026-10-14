# Fling design system — WinUI 3

This folder is the Fling design system, exported for a WinUI 3 app. The source of truth is the Fling design system artifact on claude.ai; `tokens.json` here is a copy of it.

## What's here

| Path | What |
| --- | --- |
| `Fling.Theme.xaml` | Every token as WinUI resources (Light, Dark, HighContrast), plus text, button, card, avatar and progress styles. Also recolours WinUI's accent to Fling vermilion so stock controls match. |
| `tokens.json` | The raw tokens (colours per theme, type, spacing, radii, shadows) with usage notes. |
| `Components/*.md` | Guidelines for Button, DropZone, TransferRow, DevicePicker, Toast: states, wording, what the control needs. |
| `Logos/` | The flung-dot logo as SVG: main (light grounds), dark, small, reversed (on vermilion), black, white, plus the app icon. |
| `AppIcon/` | The app icon as the PNGs the package manifest expects (all scales and target sizes), `AppIcon.ico`, and splash images. |
| `Reference/` | A screenshot and the CSS of the components as built in the design system. Use them as the visual target, not as code. |

## Setup (one time)

1. Copy this folder into the project as `Design/` (next to `App.xaml`). Make sure the `.xaml` file is a Page item, which is the default for WinUI projects. The PNGs, SVGs and the .ico should be **Content**.
2. Merge the theme in `App.xaml`, **after** `XamlControlsResources`:

   ```xml
   <Application.Resources>
     <ResourceDictionary>
       <ResourceDictionary.MergedDictionaries>
         <XamlControlsResources xmlns="using:Microsoft.UI.Xaml.Controls" />
         <ResourceDictionary Source="/Design/Fling.Theme.xaml" />
       </ResourceDictionary.MergedDictionaries>
     </ResourceDictionary>
   </Application.Resources>
   ```

3. App icon: copy the `AppIcon/*.png` files over the template's `Assets/` images (same names). In `Package.appxmanifest`, set the tile and splash `BackgroundColor` to `transparent`, or to `#BD3514` for the splash, which is a white logo. Set `<ApplicationIcon>Design\AppIcon\AppIcon.ico</ApplicationIcon>` in the `.csproj` and call `AppWindow.SetIcon("Design/AppIcon/AppIcon.ico")` so the title bar and taskbar use it.
4. Logos in XAML: `<Image Source="/Design/Logos/fling-logo.svg" Height="32" />` (WinUI renders SVG through `SvgImageSource`). Pick the file by background, as `Logos/README` says. Under 32px tall, use `fling-logo-small.svg`.

## Using the tokens

```xml
<Grid Background="{ThemeResource FlingSurfaceBrush}" Padding="{StaticResource FlingWindowPadding}">
  <Border Style="{StaticResource FlingCardStyle}">
    <StackPanel Spacing="{StaticResource FlingSpace1}">
      <TextBlock Text="take_042_final.mov" Style="{StaticResource FlingBodyTextStyle}" />
      <ProgressBar Value="62" Style="{StaticResource FlingProgressBarStyle}" />
      <TextBlock Text="482.6 MB · 62% · 38.2 MB/s · 0:05 left" Style="{StaticResource FlingMonoTextStyle}" />
    </StackPanel>
  </Border>
  <Button Content="Send" Style="{StaticResource FlingPrimaryButtonStyle}" />
</Grid>
```

- Colours: always `{ThemeResource Fling…Brush}`, never hex. They follow light, dark and high contrast automatically.
- Spacing, radii, fonts and styles: `{StaticResource …}`.
- Resource names map to tokens: `surface` → `FlingSurfaceBrush`, `ink-muted` → `FlingInkMutedBrush`, `fling` → `FlingBrush`, `space-4` → `FlingSpace4`, `radius-md` → `FlingRadiusMd`, text style `caption` → `FlingCaptionTextStyle`.

## Mica

If the window uses Mica, let it show through on the window background and use `FlingSurfaceRaisedBrush` cards on top. Without Mica, set the root to `FlingSurfaceBrush`.

## Components

The five components (DropZone, TransferRow, DevicePicker, Toast, plus the Button styles) are specified in `Components/*.md`, with the target look in `Reference/components-web.png`. The Button styles are in the theme. The other four are meant to be built as UserControls in the app, so they fit its architecture. The project's `CLAUDE.md` tells Claude Code how.
