import humanize


def greet(who):
    return f"hello from the {who} workspace, {humanize.naturaldelta(60)}"
