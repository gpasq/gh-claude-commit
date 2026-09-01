# Re-record docs/demo.gif with https://github.com/charmbracelet/vhs:
#
#   DEMO_REPO=/tmp/demo vhs docs/demo.tape
#
# DEMO_REPO should be a scratch git repository with a substantial change
# already staged — the message is generated live, so the recording is only as
# good as the diff you point it at.

Output docs/demo.gif

Require gh

Set Shell "bash"
Set FontSize 15
Set Width 1080
Set Height 600
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms

Hide
Type "cd $DEMO_REPO && export PS1='$ ' && clear"
Enter
Sleep 1s
Show

Type "git diff --cached --stat"
Enter
Sleep 2500ms

Type "gh claude-commit --commit"
Enter
Sleep 11s

Enter
Sleep 3s

# The recording is padded to Height 600; crop it down to the content with:
#   ffmpeg -i docs/demo.gif -filter_complex \
#     "crop=1080:416:0:0,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse" \
#     -loop 0 -y docs/demo.gif
