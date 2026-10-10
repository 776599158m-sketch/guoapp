import base64
from dataclasses import dataclass


@dataclass(frozen=True)
class BuildVariant:
    all_sources: bool = False
    lite: bool = False

    def __post_init__(self):
        if self.lite:
            object.__setattr__(self, 'all_sources', True)

    @property
    def name(self):
        return '真果鉴 Lite' if self.lite else '真果鉴' if self.all_sources else '红果鉴'

    @property
    def slug(self):
        return 'zhenguojian-lite' if self.lite else 'zhenguojian' if self.all_sources else 'hongguojian'

    @property
    def arguments(self):
        return ['--all-sources'] if self.all_sources else []

    @property
    def flutter_arguments(self):
        return ['--dart-define=ALL_SOURCES=' + str(self.all_sources).lower(),
                '--dart-define=LITE=' + str(self.lite).lower()]

    @property
    def linker_flags(self):
        return ('-s -w -X duanjuapp/native/core.buildAllSources=' + str(self.all_sources).lower() +
                ' -X duanjuapp/native/core.buildLite=' + str(self.lite).lower())

    @classmethod
    def from_dart_defines(cls, encoded):
        values = {}
        for item in encoded.split(','):
            if not item:
                continue
            key, separator, value = base64.b64decode(item, validate=True).decode('utf-8').partition('=')
            if separator:
                values[key] = value
        return cls(values.get('ALL_SOURCES') == 'true', values.get('LITE') == 'true')


def add_variant_argument(parser):
    parser.add_argument('--all-sources', action='store_true',
                        help='构建包含全部站源的真果鉴；默认构建仅红果的红果鉴')
